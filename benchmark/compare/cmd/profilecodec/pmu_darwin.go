//go:build darwin && cgo

package main

/*
#include <dlfcn.h>
#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

// Private ABI, isolated to this optional diagnostic. See the source-checked
// declarations and scope restrictions in PMU_METHODOLOGY_ARM64.md.
typedef struct kpep_db kpep_db;
typedef struct kpep_config kpep_config;
typedef struct kpep_event kpep_event;
#define KPC_API(X) \
 X(uint32_t,kpc_get_counting,(void)) \
 X(int,kpc_set_counting,(uint32_t)) \
 X(uint32_t,kpc_get_thread_counting,(void)) \
 X(int,kpc_set_thread_counting,(uint32_t)) \
 X(uint32_t,kpc_get_config_count,(uint32_t)) \
 X(uint32_t,kpc_get_counter_count,(uint32_t)) \
 X(int,kpc_get_config,(uint32_t,uint64_t*)) \
 X(int,kpc_set_config,(uint32_t,uint64_t*)) \
 X(int,kpc_force_all_ctrs_get,(int*)) \
 X(int,kpc_force_all_ctrs_set,(int)) \
 X(int,kpc_get_thread_counters,(uint32_t,uint32_t,uint64_t*))
#define KPEP_API(X) \
 X(int,kpep_db_create,(const char*,kpep_db**)) \
 X(void,kpep_db_free,(kpep_db*)) \
 X(int,kpep_db_event,(kpep_db*,const char*,kpep_event**)) \
 X(int,kpep_config_create,(kpep_db*,kpep_config**)) \
 X(void,kpep_config_free,(kpep_config*)) \
 X(int,kpep_config_force_counters,(kpep_config*)) \
 X(int,kpep_config_add_event,(kpep_config*,kpep_event**,uint32_t,uint32_t*)) \
 X(int,kpep_config_kpc_classes,(kpep_config*,uint32_t*)) \
 X(int,kpep_config_kpc_count,(kpep_config*,size_t*)) \
 X(int,kpep_config_kpc_map,(kpep_config*,size_t*,size_t)) \
 X(int,kpep_config_kpc,(kpep_config*,uint64_t*,size_t))
#define DECL(ret,name,args) static ret (*name) args;
KPC_API(DECL)
KPEP_API(DECL)
#undef DECL

typedef struct {
 kpep_db *db;
 kpep_config *config;
 uint32_t classes, count, old_counting, old_thread;
 int old_force, owned, saved;
 uint64_t old_config[32];
 size_t map[32];
 char error[256];
} jjs_pmu;

static void pmu_error(jjs_pmu *p, const char *name, int code) {
 snprintf(p->error,sizeof(p->error),"%s returned %d (errno %d: %s)",name,code,errno,strerror(errno));
}
static int pmu_close(jjs_pmu *p) {
 int rc=0, current;
 if(p->owned) {
  current=kpc_set_thread_counting(0); if(current && !rc) rc=current;
  current=kpc_set_counting(p->old_counting & 1u); if(current && !rc) rc=current;
  if(p->saved) { current=kpc_set_config(p->classes,p->old_config); if(current && !rc) rc=current; }
  current=kpc_force_all_ctrs_set(p->old_force); if(current && !rc) rc=current;
  // POWER counters become addressable again only after releasing ownership.
  current=kpc_set_counting(p->old_counting); if(current && !rc) rc=current;
  current=kpc_set_thread_counting(p->old_thread); if(current && !rc) rc=current;
  p->owned=0;
 }
 if(p->config) { kpep_config_free(p->config); p->config=NULL; }
 if(p->db) { kpep_db_free(p->db); p->db=NULL; }
 return rc;
}
static int pmu_open(jjs_pmu *p,int group) {
 memset(p,0,sizeof(*p));
 void *kp=dlopen("/System/Library/PrivateFrameworks/kperf.framework/kperf",RTLD_NOW);
 void *kd=dlopen("/System/Library/PrivateFrameworks/kperfdata.framework/kperfdata",RTLD_NOW);
 if(!kp || !kd) { snprintf(p->error,sizeof(p->error),"kperf/kperfdata unavailable"); return -1; }
#define LOAD_KP(ret,name,args) name=dlsym(kp,#name); if(!name) { snprintf(p->error,sizeof(p->error),"missing %s",#name); return -1; }
#define LOAD_KD(ret,name,args) name=dlsym(kd,#name); if(!name) { snprintf(p->error,sizeof(p->error),"missing %s",#name); return -1; }
 KPC_API(LOAD_KP)
 KPEP_API(LOAD_KD)
#undef LOAD_KP
#undef LOAD_KD
#define CHECK(call) do { errno=0; int check_rc=(call); if(check_rc) { pmu_error(p,#call,check_rc); goto fail; } } while(0)
 CHECK(kpep_db_create(NULL,&p->db));
 CHECK(kpep_config_create(p->db,&p->config));
 CHECK(kpep_config_force_counters(p->config));
 const char *names[5]={"Cycles","Instructions","INST_ALL",NULL,NULL};
 if(group==0) { names[3]="INST_BRANCH"; names[4]="BRANCH_MISPRED_NONSPEC"; }
 else if(group==1) { names[3]="L1D_CACHE_MISS_LD_NONSPEC"; names[4]="L1I_CACHE_MISS_DEMAND"; }
 else if(group==2) { names[3]="L1D_TLB_MISS_NONSPEC"; names[4]="L2_TLB_MISS_DATA"; }
 // INST_ALL and INST_SIMD_ALU both require counter7 on M1.
 else { names[2]="INST_SIMD_ALU"; names[3]="INST_INT_LD"; names[4]="INST_SIMD_LD"; }
 for(int i=0;i<5;i++) {
  kpep_event *event=NULL;
  CHECK(kpep_db_event(p->db,names[i],&event));
  CHECK(kpep_config_add_event(p->config,&event,i<2?0:1,NULL));
 }
 CHECK(kpep_config_kpc_classes(p->config,&p->classes));
 if((p->classes & ~3u) || !p->classes) { snprintf(p->error,sizeof(p->error),"unexpected counter classes"); goto fail; }
 size_t config_count=0;
 CHECK(kpep_config_kpc_count(p->config,&config_count));
 if(config_count>32) { snprintf(p->error,sizeof(p->error),"too many config registers"); goto fail; }
 uint64_t registers[32]={0};
 CHECK(kpep_config_kpc_map(p->config,p->map,sizeof(p->map)));
 CHECK(kpep_config_kpc(p->config,registers,sizeof(registers)));
 CHECK(kpc_force_all_ctrs_get(&p->old_force));
 p->old_counting=kpc_get_counting(); p->old_thread=kpc_get_thread_counting();
 // Fixed + OS power counters (mask5) are a normal baseline. Refuse any
 // configurable/raw/unknown capture or an existing forced/thread owner.
 if(p->old_force || (p->old_counting & ~5u) || p->old_thread) { snprintf(p->error,sizeof(p->error),"counter capture active/unavailable: force=%d counting=%u thread=%u",p->old_force,p->old_counting,p->old_thread); goto fail; }
 CHECK(kpc_force_all_ctrs_set(1)); p->owned=1;
 p->count=kpc_get_counter_count(p->classes);
 uint32_t actual_config_count=kpc_get_config_count(p->classes);
 if(p->count>32 || !p->count || actual_config_count>32 || config_count!=actual_config_count) { snprintf(p->error,sizeof(p->error),"unexpected counter/config count"); goto fail; }
 for(int i=0;i<5;i++) if(p->map[i]>=p->count) { snprintf(p->error,sizeof(p->error),"invalid counter map"); goto fail; }
 CHECK(kpc_get_config(p->classes,p->old_config)); p->saved=1;
 CHECK(kpc_set_config(p->classes,registers));
 CHECK(kpc_set_counting(p->old_counting | p->classes));
 CHECK(kpc_set_thread_counting(p->classes));
 uint64_t initial[32];
 CHECK(kpc_get_thread_counters(0,p->count,initial));
 return 0;
fail: {
 int cleanup=pmu_close(p);
 if(cleanup) { size_t len=strlen(p->error); snprintf(p->error+len,sizeof(p->error)-len,"; cleanup returned %d",cleanup); }
 return -1;
}
#undef CHECK
}
static int pmu_sample(jjs_pmu *p,uint64_t *out) {
 uint64_t values[32];
 int rc=kpc_get_thread_counters(0,p->count,values);
 if(rc) { pmu_error(p,"kpc_get_thread_counters",rc); return rc; }
 for(int i=0;i<5;i++) out[i]=values[p->map[i]];
 return 0;
}
*/
import "C"

import "fmt"

type pmuSession struct {
	state C.jjs_pmu
	names [5]string
}

func openPMU(group string) (*pmuSession, error) {
	groups := map[string][2]string{
		"branch": {"INST_BRANCH", "BRANCH_MISPRED_NONSPEC"},
		"cache":  {"L1D_CACHE_MISS_LD_NONSPEC", "L1I_CACHE_MISS_DEMAND"},
		"tlb":    {"L1D_TLB_MISS_NONSPEC", "L2_TLB_MISS_DATA"},
		"simd":   {"INST_INT_LD", "INST_SIMD_LD"},
	}
	events, ok := groups[group]
	if !ok {
		return nil, fmt.Errorf("unknown PMU group %q", group)
	}
	index := map[string]int{"branch": 0, "cache": 1, "tlb": 2, "simd": 3}[group]
	p := &pmuSession{names: [5]string{"fixed_cycles", "fixed_instructions", "user_instructions", events[0], events[1]}}
	if group == "simd" {
		p.names[2] = "INST_SIMD_ALU"
	}
	if C.pmu_open(&p.state, C.int(index)) != 0 {
		return nil, fmt.Errorf("PMU unavailable: %s", C.GoString(&p.state.error[0]))
	}
	return p, nil
}

func (p *pmuSession) sample() ([5]uint64, error) {
	var values [5]C.uint64_t
	if C.pmu_sample(&p.state, &values[0]) != 0 {
		return [5]uint64{}, fmt.Errorf("PMU snapshot: %s", C.GoString(&p.state.error[0]))
	}
	return [5]uint64{uint64(values[0]), uint64(values[1]), uint64(values[2]), uint64(values[3]), uint64(values[4])}, nil
}

func (p *pmuSession) close() error {
	if rc := C.pmu_close(&p.state); rc != 0 {
		return fmt.Errorf("restore PMU configuration: code %d", int(rc))
	}
	return nil
}
