#!/usr/bin/env python3
"""Capture warmed macOS process counters and separate Go CPU/heap profiles.

Build cmd/profilecodec first. Each library runs in its own child process;
RUSAGE_INFO_V6 snapshots surround a stdin-gated, fixed-operation loop.
The counters cover the entire process, including its Go runtime and GC.
"""

import argparse
import ctypes
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import signal
import struct
import subprocess


FIELDS = """user_time system_time pkg_idle_wkups interrupt_wkups pageins
wired_size resident_size phys_footprint proc_start_abstime proc_exit_abstime
child_user_time child_system_time child_pkg_idle_wkups child_interrupt_wkups
child_pageins child_elapsed_abstime diskio_bytesread diskio_byteswritten
cpu_time_qos_default cpu_time_qos_maintenance cpu_time_qos_background
cpu_time_qos_utility cpu_time_qos_legacy cpu_time_qos_user_initiated
cpu_time_qos_user_interactive billed_system_time serviced_system_time
logical_writes lifetime_max_phys_footprint instructions cycles billed_energy
serviced_energy interval_max_phys_footprint runnable_time flags user_ptime
system_ptime pinstructions pcycles energy_nj penergy_nj secure_time_in_system
secure_ptime_in_system neural_footprint lifetime_max_neural_footprint
interval_max_neural_footprint conclave_footprint page_wait_time_mach
page_cache_hits""".split()


class MacCounters:
    def __init__(self):
        if platform.system() != "Darwin":
            raise RuntimeError("process counters require macOS")
        self.lib = ctypes.CDLL("/usr/lib/libproc.dylib", use_errno=True)
        self.lib.proc_pid_rusage.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_void_p]
        self.lib.proc_pid_rusage.restype = ctypes.c_int
        system = ctypes.CDLL("/usr/lib/libSystem.B.dylib")
        timebase = (ctypes.c_uint32 * 2)()
        if system.mach_timebase_info(ctypes.byref(timebase)) != 0:
            raise RuntimeError("mach_timebase_info failed")
        self.numer, self.denom = timebase

    def read(self, pid):
        # The local SDK's V6 layout is a UUID followed by 50 fields and six
        # reserved uint64 words. Leave extra capacity for harmless SDK growth.
        buf = ctypes.create_string_buffer(1024)
        if self.lib.proc_pid_rusage(pid, 6, ctypes.byref(buf)) != 0:
            raise OSError(ctypes.get_errno(), "proc_pid_rusage(V6)")
        values = struct.unpack_from("=" + "Q" * len(FIELDS), buf.raw, 16)
        return dict(zip(FIELDS, values))


def command(args, codec, operation, size, count):
    cmd = [str(args.binary), "-codec", codec, "-operation", operation,
           "-size", size, "-n", str(count)]
    if args.pmu:
        cmd += ["-pmu", args.pmu]
    return cmd


def plain_run(cmd, env):
    result = subprocess.run(cmd, env=env, capture_output=True, text=True, check=True)
    return json.loads(result.stdout)


def counter_run(cmd, env, counters):
    child = subprocess.Popen(cmd + ["-counter-gate"], env=env, stdin=subprocess.PIPE,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    snapshots = []
    diagnostic = []
    try:
        for expected in ("counter-ready", "counter-done"):
            while True:
                line = child.stderr.readline()
                if not line:
                    raise RuntimeError("counter handshake ended: " + "".join(diagnostic))
                diagnostic.append(line)
                if line.strip() == expected:
                    break
            os.kill(child.pid, signal.SIGSTOP)
            _, status = os.waitpid(child.pid, os.WUNTRACED)
            if not os.WIFSTOPPED(status):
                raise RuntimeError("child did not stop at the measurement boundary")
            snapshots.append(counters.read(child.pid))
            os.kill(child.pid, signal.SIGCONT)
            child.stdin.write("\n")
            child.stdin.flush()
        stdout, stderr = child.communicate(timeout=120)
        if child.returncode:
            raise RuntimeError("codec failed: " + "".join(diagnostic) + stderr)
    finally:
        if child.poll() is None:
            os.kill(child.pid, signal.SIGCONT)
            # EOF at either gate returns through the child's PMU cleanup.
            # Let an in-flight bounded loop reach that gate before resorting
            # to termination for an unresponsive process.
            if child.stdin and not child.stdin.closed:
                child.stdin.close()
            try:
                child.wait(timeout=30)
            except subprocess.TimeoutExpired:
                child.terminate()
                child.wait(timeout=10)
    result = json.loads(stdout)
    before, after = snapshots
    names = ("instructions", "cycles", "pinstructions", "pcycles", "user_time",
             "system_time", "user_ptime", "system_ptime", "runnable_time", "pageins")
    delta = {key: after[key] - before[key] for key in names}
    if any(delta[key] < 0 for key in names) or not delta["cycles"] or not delta["instructions"]:
        raise RuntimeError("missing or non-monotonic process counters: " + repr(delta))
    if delta["pinstructions"] > delta["instructions"] or delta["pcycles"] > delta["cycles"]:
        raise RuntimeError("performance-core counts exceed process totals")
    result["counter_before"] = before
    result["counter_after"] = after
    result["counter_delta"] = delta
    result["IPC"] = delta["instructions"] / delta["cycles"]
    result["CPI"] = delta["cycles"] / delta["instructions"]
    result["instructions_per_op"] = delta["instructions"] / result["N"]
    result["cycles_per_op"] = delta["cycles"] / result["N"]
    result["performance_cycle_fraction"] = delta["pcycles"] / delta["cycles"]
    result["cpu_ns_per_op"] = (delta["user_time"] + delta["system_time"]) * counters.numer / counters.denom / result["N"]
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--phase", choices=("counters", "cpu", "allocations"), default="counters")
    parser.add_argument("--rounds", type=int, default=5)
    parser.add_argument("--seconds", type=float, default=1)
    parser.add_argument("--gomaxprocs", type=int, default=1)
    parser.add_argument("--pmu", choices=("branch", "cache", "tlb", "simd"))
    parser.add_argument("--operation", action="append", choices=("encode", "encode-reuse", "decode-reuse", "decode-fresh"))
    args = parser.parse_args()
    # Serialize root PMU captures and ordinary profile/counter captures.
    # Read access is enough for flock, even if a root capture created it.
    measurement_lock = os.fdopen(os.open("/tmp/jjs-profilecodec.measure.lock", os.O_RDONLY | os.O_CREAT, 0o644), "r")
    fcntl.flock(measurement_lock, fcntl.LOCK_EX)
    args.binary = args.binary.resolve()
    args.output.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, SONIC_NO_ASYNC_GC="1", GOMAXPROCS=str(args.gomaxprocs))
    counters = MacCounters()
    metadata = {
        "platform": platform.platform(), "phase": args.phase, "rounds": args.rounds,
        "uid": os.getuid(),
        "target_seconds": args.seconds, "gomaxprocs": args.gomaxprocs,
        "binary": str(args.binary), "sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest(),
        "mach_timebase": {"numer": counters.numer, "denom": counters.denom},
        "counter_source": "proc_pid_rusage RUSAGE_INFO_V6; process scope",
        "worker_counter_group": args.pmu,
        "handshake": "stopped-child snapshots bracket the loop plus bounded pipe/signal/format/readMemStats overhead",
    }
    (args.output / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
    operations = args.operation or ["encode", "encode-reuse", "decode-reuse", "decode-fresh"]
    counts = {}
    with (args.output / "calibration.jsonl").open("w") as raw:
        for operation in operations:
            for size in ("Small", "Medium", "Large"):
                for codec in ("jjs", "sonic"):
                    record = plain_run(command(args, codec, operation, size, {"Small": 10000, "Medium": 2000, "Large": 100}[size]), env)
                    count = max(1, round(args.seconds * 1e9 / record["NSPerOp"]))
                    counts[codec, operation, size] = count
                    raw.write(json.dumps(record) + "\n")
    with (args.output / "runs.jsonl").open("w") as raw:
        for round_index in range(args.rounds):
            order = ("jjs", "sonic") if round_index % 2 == 0 else ("sonic", "jjs")
            for operation in operations:
                for size in ("Small", "Medium", "Large"):
                    for codec in order:
                        count = counts[codec, operation, size]
                        cmd = command(args, codec, operation, size, count)
                        if args.phase == "counters":
                            record = counter_run(cmd, env, counters)
                        else:
                            profiles = args.output / f"{round_index + 1}-{operation}-{size}-{codec}"
                            cmd += ["-profiles", str(profiles)]
                            if args.phase == "allocations":
                                cmd += ["-cpu=false", "-mem-rate=1"]
                            record = plain_run(cmd, env)
                            record["profile_dir"] = str(profiles)
                        record["round"] = round_index + 1
                        raw.write(json.dumps(record) + "\n")
                        raw.flush()
                        print(round_index + 1, operation, size, codec,
                              f"{record['NSPerOp']:.2f} ns/op", flush=True)
    measurement_lock.close()


if __name__ == "__main__":
    main()
