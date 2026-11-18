#!/usr/bin/env python3
"""Run native/httpx servers sequentially under Vegeta's fixed-rate load."""
import argparse
import json
import os
from pathlib import Path
import random
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
GO = os.environ.get('GO', 'go')


def resolve_vegeta(explicit: str | None) -> str:
    candidates = []
    if explicit:
        candidates.append(explicit)
    which = shutil.which('vegeta')
    if which:
        candidates.append(which)
    if gobin := os.environ.get('GOBIN'):
        candidates.append(str(Path(gobin) / 'vegeta'))
    if gopath := os.environ.get('GOPATH'):
        for entry in gopath.split(os.pathsep):
            if entry:
                candidates.append(str(Path(entry) / 'bin' / 'vegeta'))
    candidates.append(str(Path.home() / 'go/bin/vegeta'))
    for candidate in candidates:
        p = Path(candidate).expanduser()
        if p.is_file() and os.access(p, os.X_OK):
            return str(p)
    sys.exit('vegeta executable not found (checked --vegeta, PATH, GOBIN, GOPATH/bin, ~/go/bin/vegeta)')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vegeta', help='path to vegeta executable')
    parser.add_argument('--binary', type=Path, help='precompiled conformance test binary')
    parser.add_argument('--rate', type=int, default=5000)
    parser.add_argument('--duration', default='3s')
    parser.add_argument('--repeat', type=int, default=3)
    parser.add_argument('--cpu', type=int, default=4, help='GOMAXPROCS for servers and load generator (default: 4)')
    parser.add_argument('--output', type=Path, default=ROOT / 'benchmarks/results/latest/network.json')
    args = parser.parse_args()
    if args.rate <= 0 or args.repeat <= 0 or args.cpu <= 0:
        parser.error('rate, repeat and cpu must be positive')
    vegeta_bin = resolve_vegeta(args.vegeta)
    if args.binary:
        args.binary = args.binary.resolve()
    env = dict(os.environ, GOMAXPROCS=str(args.cpu))
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with tempfile.TemporaryDirectory(prefix='httpx-network-') as tmp:
        binary = args.binary or Path(tmp) / 'server.test'
        if not args.binary:
            subprocess.run([GO, 'test', '-c', './conformance', '-o', str(binary)], cwd=ROOT, check=True)
        results = []
        jobs = [(f, s, m) for f in ('gin', 'echo', 'fiber', 'hertz', 'std')
                for s in ('Empty', 'JSON1K') for m in ('native', 'httpx')]
        random.Random(42).shuffle(jobs)
        for framework, scenario, mode in jobs:
            with socket.socket() as reservation:
                reservation.bind(('127.0.0.1', 0))
                addr = f'127.0.0.1:{reservation.getsockname()[1]}'
            # The short bind/close/start gap is checked by the readiness probe.
            cmd = [str(binary), '-test.run=^TestAdapterBenchmarkServer$',
                   f'-httpx-bench-addr={addr}', f'-httpx-bench-framework={framework}',
                   f'-httpx-bench-mode={mode}', f'-httpx-bench-scenario={scenario}']
            with tempfile.TemporaryFile() as log:
                server = subprocess.Popen(cmd, cwd=ROOT, env=env, stdout=log, stderr=log)
                try:
                    target = f'http://{addr}/bench'
                    ready = False
                    for _ in range(100):
                        if server.poll() is not None:
                            log.seek(0)
                            raise RuntimeError(log.read().decode(errors='replace'))
                        try:
                            with opener.open(target, timeout=.5) as response:
                                body = response.read()
                                if scenario == 'JSON1K':
                                    if response.status != 200 or json.loads(body) != {'id': 42, 'name': 'benchmark', 'data': 'x' * 1024}:
                                        raise RuntimeError(f'unexpected JSON1K readiness response: status={response.status}')
                                elif response.status != 204 or body:
                                    raise RuntimeError(f'unexpected Empty readiness response: status={response.status} len={len(body)}')
                            ready = True
                            break
                        except urllib.error.HTTPError as exc:
                            raise RuntimeError(f'readiness probe failed for {target}: HTTP {exc.code}') from exc
                        except (OSError, TimeoutError):
                            time.sleep(.05)
                    if not ready:
                        raise RuntimeError(f'server did not become ready: {cmd}')
                    for sample in range(args.repeat + 1):
                        attack = [vegeta_bin, 'attack', f'-rate={args.rate}/s',
                                  f'-duration={"1s" if sample == 0 else args.duration}',
                                  '-workers=16', '-max-workers=16', '-connections=16',
                                  '-max-connections=16', '-http2=false', '-timeout=2s']
                        raw = subprocess.run(attack, input=f'GET {target}\n'.encode(),
                                             stdout=subprocess.PIPE, check=True, env=env).stdout
                        metrics = json.loads(subprocess.check_output(
                            [vegeta_bin, 'report', '-type=json'], input=raw, env=env))
                        expected = '200' if scenario == 'JSON1K' else '204'
                        if metrics['success'] != 1 or set(metrics['status_codes']) != {expected}:
                            raise RuntimeError(f'failed requests: {metrics}')
                        if sample:
                            results.append(dict(framework=framework, scenario=scenario,
                                                mode=mode, sample=sample, metrics=metrics))
                    print(f'{framework} {scenario} {mode}: {args.repeat} samples, 100% success', flush=True)
                finally:
                    server.terminate()
                    try:
                        server.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        server.kill()
                        server.wait()
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(dict(rate=args.rate, duration=args.duration,
                                               repeat=args.repeat, gomaxprocs=args.cpu,
                                               client_workers=16, results=results), indent=2) + '\n')


if __name__ == '__main__':
    main()
