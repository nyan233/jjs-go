//go:build darwin && cgo

package main

/*
#include <pthread.h>
#include <pthread/qos.h>

static int worker_qos(int *previous, int *previous_relative, int *requested, int *relative) {
	qos_class_t before, after;
	int rc = pthread_get_qos_class_np(pthread_self(), &before, previous_relative);
	if (rc) return rc;
	*previous = (int)before;
	rc = pthread_set_qos_class_self_np(QOS_CLASS_USER_INITIATED, 0);
	if (rc) return rc;
	rc = pthread_get_qos_class_np(pthread_self(), &after, relative);
	if (rc) return rc;
	*requested = (int)after;
	return 0;
}
*/
import "C"

import "fmt"

// requestWorkerQoS applies only to the current locked OS thread. The caller
// must keep that thread locked until this short-lived process exits. Requested
// QoS is not effective priority or P-core affinity; measure actual residency.
func requestWorkerQoS(name string) (map[string]int, error) {
	if name != "user-initiated" {
		return nil, fmt.Errorf("unknown worker QoS %q", name)
	}
	var before, beforeRelative, after, relative C.int
	if rc := C.worker_qos(&before, &beforeRelative, &after, &relative); rc != 0 {
		return nil, fmt.Errorf("worker QoS request failed: pthread error %d", int(rc))
	}
	return map[string]int{
		"previous_class": int(before), "previous_relative_priority": int(beforeRelative),
		"requested_class": int(after), "requested_relative_priority": int(relative),
	}, nil
}
