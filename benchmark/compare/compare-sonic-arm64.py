#!/usr/bin/env python3
"""Compare prebuilt release/dev Sonic and jjs with identical codec workloads.

Uses profile-macos.py for stopped-child process counters. The dev binary is
also run with its VM forced, isolating the encoder backend at one commit.
"""

import argparse
import csv
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import statistics


SONIC_ENV = ("SONIC_ENCODER_USE_VM", "SONIC_USE_SVE_WRAPGOC",
             "SONIC_USE_SVE_LINKNAME", "SONIC_USE_OPTDEC", "SONIC_USE_FASTMAP",
             "SONIC_SYNC_GC", "SONIC_CHECK_POINTER")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release-binary", type=Path, required=True)
    parser.add_argument("--dev-binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--rounds", type=int, default=5)
    parser.add_argument("--seconds", type=float, default=1)
    args = parser.parse_args()
    if args.rounds < 1 or args.seconds <= 0:
        parser.error("rounds and seconds must be positive")
    spec = importlib.util.spec_from_file_location("capture", Path(__file__).with_name("profile-macos.py"))
    capture = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(capture)
    args.output.mkdir(parents=True, exist_ok=True)
    lock = os.fdopen(os.open("/tmp/jjs-profilecodec.measure.lock", os.O_RDONLY | os.O_CREAT, 0o644), "r")
    fcntl.flock(lock, fcntl.LOCK_EX)
    env = dict(os.environ, GOMAXPROCS="1", SONIC_NO_ASYNC_GC="1")
    removed = {key: env.pop(key) for key in SONIC_ENV if key in env}
    release, dev = args.release_binary.resolve(), args.dev_binary.resolve()
    variants = [("jjs", release, "jjs", False),
                ("sonic-v1.15.5", release, "sonic", False),
                ("sonic-dev-vm", dev, "sonic", True),
                ("sonic-dev-jit", dev, "sonic", False)]
    counters = capture.MacCounters()
    metadata = {
        "platform": platform.platform(), "uid": os.getuid(),
        "rounds": args.rounds, "target_seconds": args.seconds,
        "GOMAXPROCS": 1, "SONIC_NO_ASYNC_GC": "1", "cleared_environment": removed,
        "mach_timebase": {"numer": counters.numer, "denom": counters.denom},
        "variants": [{"name": name, "binary": str(binary), "codec": codec,
                      "force_vm": vm, "sha256": hashlib.sha256(binary.read_bytes()).hexdigest()}
                     for name, binary, codec, vm in variants],
        "order": "rotate four variants by round; reverse on alternate rounds",
        "scope": "process user+kernel, all threads; no privileged KPC or CPU sampling",
    }
    (args.output / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")

    def command(variant, operation, size, count):
        name, binary, codec, vm = variant
        child_env = dict(env)
        if vm:
            child_env["SONIC_ENCODER_USE_VM"] = "1"
        return [str(binary), "-codec", codec, "-operation", operation,
                "-size", size, "-n", str(count)], child_env

    cases = [(op, size) for op in ("encode", "encode-reuse", "decode-reuse", "decode-fresh")
             for size in ("Small", "Medium", "Large")]
    counts = {}
    with (args.output / "calibration.jsonl").open("w") as output:
        for operation, size in cases:
            for variant in variants:
                cmd, child_env = command(variant, operation, size, {"Small": 10000, "Medium": 2000, "Large": 100}[size])
                record = capture.plain_run(cmd, child_env)
                record["variant"] = variant[0]
                output.write(json.dumps(record) + "\n")
                output.flush()
                counts[variant[0], operation, size] = max(1, round(args.seconds * 1e9 / record["NSPerOp"]))
    records = []
    with (args.output / "runs.jsonl").open("w") as output:
        for round_index in range(args.rounds):
            offset = round_index % len(variants)
            order = variants[offset:] + variants[:offset]
            if round_index % 2:
                order = order[::-1]
            for operation, size in cases:
                for variant in order:
                    cmd, child_env = command(variant, operation, size, counts[variant[0], operation, size])
                    record = capture.counter_run(cmd, child_env, counters)
                    record.update(variant=variant[0], round=round_index + 1)
                    output.write(json.dumps(record) + "\n")
                    output.flush()
                    records.append(record)
                    print(round_index + 1, operation, size, variant[0],
                          f"{record['NSPerOp']:.2f} ns/op P={record['performance_cycle_fraction']:.4f}", flush=True)
    rows = []
    for operation, size in cases:
        for variant in variants:
            rs = [r for r in records if (r["Operation"], r["Size"], r["variant"]) == (operation, size, variant[0])]
            row = {"operation": operation, "size": size, "variant": variant[0], "samples": len(rs)}
            for key in ("NSPerOp", "BytesPerOp", "AllocsPerOp", "instructions_per_op", "cycles_per_op"):
                row[key] = statistics.median(r[key] for r in rs)
            row["min_NSPerOp"] = min(r["NSPerOp"] for r in rs)
            row["max_NSPerOp"] = max(r["NSPerOp"] for r in rs)
            row["IPC"] = sum(r["counter_delta"]["instructions"] for r in rs) / sum(r["counter_delta"]["cycles"] for r in rs)
            row["CPI"] = 1 / row["IPC"]
            row["min_P_cycle_fraction"] = min(r["performance_cycle_fraction"] for r in rs)
            rows.append(row)
    with (args.output / "summary.csv").open("w") as output:
        writer = csv.DictWriter(output, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    lock.close()


if __name__ == "__main__":
    main()
