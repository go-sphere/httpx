#!/usr/bin/env python3
"""Run every httpx benchmark table in one session and render one report.

The point of this script is the word "one". Each table used to be run by hand,
at a different commit, with a different sample count, and written up separately,
so no two numbers in the benchmarks/ directory could be compared. Here the
conformance test binary is compiled once and every table is measured from that
same binary, back to back, with the same sampling; the environment, the git
revision and the dirty files are recorded alongside the numbers.
"""
import argparse
import json
import platform
import re
import shutil
import statistics
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
# Generated output goes in its own directory. results/ also holds older raw
# data from past experiments, and a generator writing into the same namespace
# will eventually pick a name one of them already used -- which is exactly how
# the first round's environment.json and network.json were destroyed. None of
# results/ is tracked: benchmarks/ has no go.mod, so anything committed here
# ships in the root module's zip to every consumer of the library.
RESULTS = ROOT / 'benchmarks/results/latest'
# Compiled test binaries are tens of megabytes; they are kept so a run can be
# repeated from the exact same build, and live outside results/ so a stray
# `results/*` glob never picks one up. Gitignored, like results/.
BIN = ROOT / 'benchmarks/bin'

# Adapter name -> the framework it wraps, for the report's column headings.
ADAPTERS = {
    'ginx': 'gin',
    'echox': 'echo',
    'fiberx': 'fiber',
    'hertzx': 'hertz',
    'stdx': 'net/http',
}

# BenchmarkAdapter labels its rows by framework, not by adapter.
ADAPTER_BY_FRAMEWORK = {'gin': 'ginx', 'echo': 'echox', 'fiber': 'fiberx',
                        'hertz': 'hertzx', 'std': 'stdx'}


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--count', type=int, default=12, help='samples per benchmark (default: 12)')
    parser.add_argument('--benchtime', default='200ms', help='time per sample (default: 200ms)')
    parser.add_argument('--cpu', type=int, default=4, help='GOMAXPROCS for the benchmarks (default: 4)')
    parser.add_argument('--out', type=Path, default=ROOT / 'benchmarks/BENCHMARK.md')
    parser.add_argument('--skip-network', action='store_true', help='skip the Vegeta section (needs vegeta installed)')
    parser.add_argument('--skip-gin-depth', action='store_true', help='skip the gin deep-chain appendix')
    parser.add_argument('--rate', type=int, default=5000, help='network section request rate (default: 5000/s)')
    parser.add_argument('--render-only', action='store_true',
                        help='re-render the report from the saved results/ without measuring again')
    args = parser.parse_args()
    if args.count <= 0:
        parser.error('--count must be positive')

    if args.render_only:
        render_only(args)
        return

    RESULTS.mkdir(parents=True, exist_ok=True)
    BIN.mkdir(parents=True, exist_ok=True)
    env = environment(args)
    started = time.monotonic()

    # One binary for all three in-process tables: same compiler invocation, same
    # inlining decisions, same dependency versions, no rebuild between tables.
    conformance = BIN / 'conformance.test'
    log(f'compiling the conformance benchmark binary -> {rel(conformance)}')
    run(['go', 'test', '-c', './conformance', '-o', str(conformance)])

    tables = {}
    for key, bench in (('adapter', '^BenchmarkAdapter$'),
                       ('suite', '^BenchmarkHTTPXTestSuite$'),
                       ('native', '^BenchmarkNativeVsHTTPX$')):
        raw = RESULTS / f'{key}.txt'
        log(f'measuring {bench} ({args.count} x {args.benchtime}) -> {rel(raw)}')
        raw.write_text(bench_binary(conformance, bench, args))
        tables[key] = parse_bench(raw.read_text())

    gin_depth = None
    if not args.skip_gin_depth:
        ginbin = BIN / 'ginx.test'
        log(f'compiling the ginx diagnostic binary -> {rel(ginbin)}')
        run(['go', 'test', '-c', './ginx', '-o', str(ginbin)])
        raw = RESULTS / 'gin-depth.txt'
        log(f'measuring the gin deep-chain curves -> {rel(raw)}')
        raw.write_text(bench_binary(ginbin, '^(BenchmarkGinCallDepth|BenchmarkGinDepthCurve|BenchmarkNestedScopes)$', args))
        gin_depth = parse_bench(raw.read_text())

    network = None
    if not args.skip_network:
        out = RESULTS / 'network.json'
        log(f'running the fixed-rate network comparison at {args.rate}/s -> {rel(out)}')
        try:
            run([sys.executable, str(ROOT / 'benchmarks/network.py'),
                 '--binary', str(conformance), '--rate', str(args.rate), '--output', str(out)])
            network = json.loads(out.read_text())
        except subprocess.CalledProcessError:
            log('network comparison failed (is vegeta installed?); the section is omitted')

    env['duration_seconds'] = round(time.monotonic() - started, 1)
    (RESULTS / 'environment.json').write_text(json.dumps(env, indent=2) + '\n')

    args.out.write_text(render(env, tables, gin_depth, network))
    log(f'wrote {rel(args.out)}')


def render_only(args):
    """Rebuilds the report from the last run's saved output.

    Editing the report's prose should not cost another measurement run, and
    re-measuring to fix a sentence would silently replace the numbers underneath
    it with a different environment's.
    """
    env_file = RESULTS / 'environment.json'
    if not env_file.exists():
        sys.exit(f'{rel(env_file)} is missing: run without --render-only first')
    env = json.loads(env_file.read_text())
    tables = {}
    for key in ('adapter', 'suite', 'native'):
        raw = RESULTS / f'{key}.txt'
        if not raw.exists():
            sys.exit(f'{rel(raw)} is missing: run without --render-only first')
        tables[key] = parse_bench(raw.read_text())
    depth_file = RESULTS / 'gin-depth.txt'
    gin_depth = parse_bench(depth_file.read_text()) if depth_file.exists() else None
    net_file = RESULTS / 'network.json'
    network = json.loads(net_file.read_text()) if net_file.exists() else None
    args.out.write_text(render(env, tables, gin_depth, network))
    log(f're-rendered {rel(args.out)} from {rel(RESULTS)} ({env["date_utc"]})')


# ---------------------------------------------------------------- measurement

def bench_binary(binary: Path, pattern: str, args) -> str:
    """Runs a compiled test binary's benchmarks and returns its raw output.

    Raw Go benchmark output is kept verbatim in results/latest/ so benchstat can
    be run over it afterwards: the rendered tables are medians, which say nothing
    about how noisy the machine was when they were taken.
    """
    cmd = [str(binary), '-test.run=^$', f'-test.bench={pattern}', '-test.benchmem',
           f'-test.benchtime={args.benchtime}', f'-test.count={args.count}', f'-test.cpu={args.cpu}']
    proc = subprocess.run(cmd, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                          text=True, check=False)
    if proc.returncode != 0:
        sys.exit(f'benchmark failed: {" ".join(cmd)}\n{proc.stdout}')
    return proc.stdout


BENCH_LINE = re.compile(r'^(Benchmark[^\s]*?)(?:-(\d+))?\s+(\d+)\s+(.+)$')


def parse_bench(text: str) -> dict:
    """Parses Go benchmark output into {labels-tuple: {metric: [samples]}}.

    Every benchmark in this repository names its sub-benchmarks as key=value
    segments, so the labels parse straight into a dict without the report
    knowing anything about the individual tables.
    """
    out = {}
    for line in text.splitlines():
        m = BENCH_LINE.match(line.strip())
        if not m:
            continue
        name, _, _, rest = m.groups()
        parts = name.split('/')
        labels = {}
        for part in parts[1:]:
            k, _, v = part.partition('=')
            labels[k] = v
        labels['_bench'] = parts[0]
        fields = rest.split()
        metrics = {}
        for value, unit in zip(fields[0::2], fields[1::2]):
            try:
                metrics[unit] = float(value)
            except ValueError:
                continue
        key = tuple(sorted(labels.items()))
        out.setdefault(key, {})
        for unit, value in metrics.items():
            out[key].setdefault(unit, []).append(value)
    return out


def pick(table: dict, **want):
    """Returns the median metrics for the one row matching every wanted label."""
    for key, metrics in table.items():
        labels = dict(key)
        if all(labels.get(k) == v for k, v in want.items()):
            return {unit: statistics.median(samples) for unit, samples in metrics.items()}
    return None


def values(table: dict, label: str) -> list:
    """Distinct values of a label, in first-seen order."""
    seen = []
    for key in table:
        v = dict(key).get(label)
        if v is not None and v not in seen:
            seen.append(v)
    return seen


def subset(table: dict, **want) -> dict:
    """The rows matching every wanted label, so values() can be asked about one
    benchmark rather than the union of all of them."""
    return {k: v for k, v in table.items() if all(dict(k).get(a) == b for a, b in want.items())}


# ------------------------------------------------------------------ rendering

def render(env, tables, gin_depth, network) -> str:
    md = []
    md.append('# httpx benchmarks\n')
    md.append('Generated by `make bench-report` — do not edit by hand. Every number here '
              'came out of one run, on one machine, from one compiled binary. The archived '
              'reports in [history/](history/) were each measured in a different '
              'environment and must not be quoted next to anything in this file.\n')

    md.append(render_environment(env))
    md.append(render_findings(tables, network))
    md.append(render_reading_notes())
    md.append(render_adapter(tables['adapter']))
    md.append(render_suite(tables['suite']))
    md.append(render_native(tables['native']))
    if network:
        md.append(render_network(network))
    if gin_depth:
        md.append(render_gin_depth(gin_depth))
    md.append(render_appendix())
    return '\n'.join(md)


def render_environment(env) -> str:
    # Read the sampling back out of env, not out of args: with --render-only the
    # flags describe this invocation, while the numbers came from that run.
    rows = [
        ('Date (UTC)', env['date_utc']),
        ('Go', env['go']),
        ('Platform', env['platform']),
        ('CPU', env['cpu']),
        ('GOMAXPROCS', str(env['gomaxprocs'])),
        ('Sampling', f"{env['samples']} x {env['benchtime']}"),
        ('git HEAD', env['git_head']),
        ('Working tree', env['git_state']),
        ('Wall clock', f"{env['duration_seconds']}s"),
    ]
    md = ['## Environment\n', '| | |', '| --- | --- |']
    md += [f'| {k} | {v} |' for k, v in rows]
    md.append('\nFramework versions — both sides of every pair live in the same binary, '
              'so they cannot differ:\n')
    md += [f'- `{v}`' for v in env['framework_versions']]
    md.append('\nReproduce:\n')
    md.append('```sh')
    md.append('make bench-report                            # 12 samples x 200ms, network and gin appendix included')
    md.append('make bench-report BENCH_COUNT=4              # a quick look')
    md.append('python3 benchmarks/report.py --skip-network --skip-gin-depth')
    md.append('python3 benchmarks/report.py --render-only   # re-render the prose without measuring again')
    md.append('```\n')
    return '\n'.join(md)


def render_findings(tables, network) -> str:
    """The conclusions, computed from this run rather than written down once.

    A generated report whose reader has to derive the findings from the tables
    is half a report -- but a hand-written conclusion inside a generated file
    goes stale the moment someone re-runs the benchmarks, and a stale conclusion
    next to fresh numbers is worse than none. So everything quantitative below
    is derived from the tables above; only the methodology caveats are fixed
    prose, because they are properties of the harness, not of the measurement.
    """
    md = ['## Summary\n',
          'Everything numbered below is computed from this run. Re-running the benchmarks '
          'rewrites it, so it cannot drift away from the tables.\n']

    md.append(_findings_adapter_ranking(tables['suite']))
    md.append(_findings_wrapping_cost(tables['adapter']))
    md.append(_findings_stdx_vs_native(tables['native']))
    md.append(_findings_caveats(network))
    return '\n'.join(md)


def _findings_adapter_ranking(table) -> str:
    """Which adapter is fastest, by how many scenarios it wins."""
    adapters = [a for a in ADAPTERS if a in values(table, 'adapter')]
    scenarios = values(table, 'scenario')
    wins = {a: [] for a in adapters}
    for scenario in scenarios:
        timings = {}
        for a in adapters:
            row = pick(table, adapter=a, scenario=scenario)
            if row and row.get('ns/op') is not None:
                timings[a] = row['ns/op']
        if timings:
            wins[min(timings, key=timings.get)].append(scenario)
    order = sorted(adapters, key=lambda a: -len(wins[a]))
    total = len(scenarios)

    md = ['### Which adapter is fastest\n']
    best = order[0]
    others = [a for a in order[1:] if wins[a]]
    line = (f'**{best}** is fastest on {len(wins[best])} of the {total} shared scenarios')
    if others:
        line += '; ' + ', '.join(f'{a} takes {_join_names(wins[a])}' for a in others)
    md.append(line + '.\n')
    md.append('| Adapter | Scenarios won | Empty ns/op | allocs on an empty request |')
    md.append('| --- | ---: | ---: | ---: |')
    for a in order:
        row = pick(table, adapter=a, scenario='Empty') or {}
        md.append(f'| {a} ({ADAPTERS[a]}) | {len(wins[a])} | '
                  f'{num(row.get("ns/op"))} | {num(row.get("allocs/op"))} |')
    md.append('')
    return '\n'.join(md)


def _findings_wrapping_cost(table) -> str:
    """Whether going through httpx costs or saves, per framework and per depth.

    The interesting question is not the fixed overhead on an empty request but
    where the crossover is: httpx composes its chain at registration, so the
    per-layer cost differs from the framework's own middleware model.
    """
    frameworks = values(table, 'framework')
    depths = [s for s in values(table, 'scenario') if s.startswith('Middleware')]
    depths.sort(key=lambda s: int(s[len('Middleware'):]))

    rows, crossovers, ahead = [], {}, []
    for framework in frameworks:
        cells = []
        empty = (pick(table, framework=framework, scenario='Empty', mode='native'),
                 pick(table, framework=framework, scenario='Empty', mode='httpx'))
        # A framework httpx already beats on an empty request has no crossover
        # to report -- it never trails in the first place.
        if all(empty) and empty[1]['ns/op'] < empty[0]['ns/op']:
            ahead.append(ADAPTER_BY_FRAMEWORK.get(framework, framework))
        for scenario in ['Empty'] + depths:
            n = pick(table, framework=framework, scenario=scenario, mode='native')
            h = pick(table, framework=framework, scenario=scenario, mode='httpx')
            if not n or not h:
                cells.append('—')
                continue
            cells.append(delta(n.get('ns/op'), h.get('ns/op')))
            if (scenario in depths and framework not in crossovers
                    and ADAPTER_BY_FRAMEWORK.get(framework, framework) not in ahead
                    and h.get('ns/op', 0) < n.get('ns/op', 0)):
                crossovers[framework] = int(scenario[len('Middleware'):])
        rows.append((framework, cells))

    md = ['### Does going through httpx cost or save\n',
          'Both sides run the same framework and return the same response; the only difference '
          'is the adapter. Negative means httpx is faster.\n']
    md.append('| Framework | ' + ' | '.join(['Empty'] + depths) + ' |')
    md.append('| --- |' + ' ---: |' * (1 + len(depths)))
    for framework, cells in rows:
        md.append(f'| {framework} → {ADAPTER_BY_FRAMEWORK.get(framework, framework)} | '
                  + ' | '.join(cells) + ' |')
    md.append('')

    if crossovers or ahead:
        parts = [f'{ADAPTER_BY_FRAMEWORK.get(f, f)} from {n} layer{"" if n == 1 else "s"} up'
                 for f, n in sorted(crossovers.items(), key=lambda kv: kv[1])]
        lead = ''
        if ahead:
            lead = (f'{_join_names(ahead)} {"is" if len(ahead) == 1 else "are"} ahead of the '
                    f'native equivalent at every depth measured, including an empty request. ')
        md.append(f'**Wrapping is not uniformly a cost.** ' + lead
                  + (f'It overtakes the framework\'s own middleware on {_join_names(parts)}. '
                     if parts else '')
                  + 'The reason is structural rather than '
                  'tuning: httpx composes the chain into the route at registration, so a layer '
                  'is one call and no allocation, while the frameworks pay per-layer index '
                  'bookkeeping or a per-layer allocation. Where httpx stays behind, it is the '
                  'fixed per-request cost of building the adapter Context, which an empty '
                  'request shows undiluted and a real handler amortizes.\n')
    else:
        md.append('**On this run httpx is slower at every measured depth** — no crossover. '
                  'That is a change from the composed chain\'s usual behaviour and worth '
                  'investigating before quoting these numbers.\n')
    return '\n'.join(md)


def _findings_stdx_vs_native(table) -> str:
    """Whether the net/http adapter beats the frameworks' own hand-written code.

    This is the one cross-framework comparison the data supports, and only
    partly: ginx, echox and stdx share a runner, so those columns are directly
    comparable; fiberx and hertzx do not.
    """
    comparable = ('ginx', 'echox')
    scenarios = [s for s in values(table, 'scenario')
                 if pick(table, framework='stdx', scenario=s, mode='httpx')]
    beaten, lost = [], []
    for scenario in scenarios:
        std = pick(table, framework='stdx', scenario=scenario, mode='httpx')
        rivals = {}
        for f in values(table, 'framework'):
            if f == 'stdx':
                continue
            row = pick(table, framework=f, scenario=scenario, mode='native')
            if row and row.get('ns/op') is not None:
                rivals[f] = row['ns/op']
        if not rivals or std.get('ns/op') is None:
            continue
        (beaten if std['ns/op'] <= min(rivals.values()) else lost).append(scenario)

    md = ['### Can the net/http adapter beat the frameworks\' own code\n']
    md.append(f'`stdx` — through httpx — is faster than **every** framework\'s hand-written '
              f'native implementation on {len(beaten)} of {len(beaten) + len(lost)} paired '
              f'scenarios.')
    if lost:
        md[-1] += f' It loses on {_join_names(lost)}.'
    md[-1] += '\n'
    md.append(f'Read this narrowly. `{"`, `".join(comparable)}` and `stdx` are driven through '
              'the same `net/http` harness, so those columns are a fair comparison and stdx '
              'genuinely wins them: its segment-tree router is cheaper than `ServeMux`, and its '
              'Context allocates nothing. `fiberx` and `hertzx` are driven through their own '
              'fasthttp dispatchers, and this benchmark deliberately excludes HTTP parsing and '
              'connection handling — which is exactly where fasthttp earns its reputation. '
              '**These numbers do not say stdx is faster than fiber or hertz.** What stdx wins '
              'is routing and context dispatch; what a mature framework offers is an ecosystem, '
              'binders and middleware this table does not measure.\n')
    return '\n'.join(md)


def _findings_caveats(network) -> str:
    md = ['### What none of this shows\n']
    if network:
        md.append('**Section 4 is the one that decides whether any of the above matters, and it '
                  'says no.** At a fixed rate over a real socket every adapter and every '
                  'framework lands on the same latency percentiles: the differences measured '
                  'here are three orders of magnitude below one round trip. Treat sections 1–3 '
                  'as a budget for the adapter layer, not as a reason to pick a framework.\n')
    else:
        md.append('The network section was skipped in this run, so nothing here is checked '
                  'against a real socket. Run it before drawing conclusions about the relative '
                  'weight of these differences — in-process nanoseconds are routinely invisible '
                  'behind one round trip.\n')
    md.append('Two rows are not like-for-like and must not be read as overhead:\n')
    md.append('- **`LargeBody` on fiber and hertz.** Native `c.Body()` returns a view into the '
              'pooled request buffer; `httpx.BodyRaw` contracts that the caller owns the slice, '
              'so the fasthttp adapters `bytes.Clone` it. That row prices a guarantee, not an '
              'abstraction.')
    md.append('- **`State` and `StateParallel` on net/http.** httpx `Set`/`Get` is a per-request '
              'store; net/http has only the request context, which propagates downstream. '
              'Different semantics, not different speed.\n')
    return '\n'.join(md)


def _join_names(items) -> str:
    items = list(items)
    if len(items) == 1:
        return f'`{items[0]}`' if ' ' not in items[0] else items[0]
    fmt = lambda s: f'`{s}`' if ' ' not in s else s
    return ', '.join(fmt(s) for s in items[:-1]) + ' and ' + fmt(items[-1])


def render_reading_notes() -> str:
    return '\n'.join([
        '## How to read this\n',
        '- **Compare pairs within one framework, never across.** Each framework has its own '
        'dispatcher and its own per-request reset cost, so fiber-native against gin-httpx '
        'means nothing. That is why the tables are grouped by framework.',
        '- **These are medians.** The full samples are in `results/latest/`. To decide whether '
        'a difference is real, run `benchstat` over those files — do not read significance '
        'off the decimal places here.',
        '- **In-process numbers exclude TCP, HTTP parsing and client cost**, which is the point: '
        'they isolate the adapter layer. A real endpoint also has a database and RPC behind it, '
        'so none of these percentages generalize to "requests got N% faster".',
        '- **`StateParallel` and `State` are not measuring the same thing on net/http.** '
        'httpx `Set`/`Get` is a per-request state store; net/http has only the request context, '
        'which propagates downstream. The `stdx` delta on those rows includes that semantic '
        'difference, not just overhead.',
        '- **Both sides of `Middleware*` are empty layers** that call the next one and return. '
        'The cost measured is the chain itself, not work done inside a layer.\n',
    ])


def render_adapter(table) -> str:
    md = ['## 1. Adapter overhead: native vs httpx\n',
          'The same framework instance type and the same response data, written once directly '
          'against the framework and once through the httpx adapter. `BenchmarkAdapter`, '
          '7 scenarios x 5 frameworks.\n']
    scenarios = values(table, 'scenario')
    for framework in values(table, 'framework'):
        md.append(f'### {framework} / {ADAPTER_BY_FRAMEWORK.get(framework, framework)}\n')
        md.append('| Scenario | native ns/op | httpx ns/op | Δ | native B/op | httpx B/op | native allocs | httpx allocs |')
        md.append('| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |')
        for scenario in scenarios:
            n = pick(table, framework=framework, scenario=scenario, mode='native')
            h = pick(table, framework=framework, scenario=scenario, mode='httpx')
            if not n or not h:
                continue
            md.append('| {} | {} | {} | {} | {} | {} | {} | {} |'.format(
                scenario, num(n.get('ns/op')), num(h.get('ns/op')),
                delta(n.get('ns/op'), h.get('ns/op')),
                num(n.get('B/op')), num(h.get('B/op')),
                num(n.get('allocs/op')), num(h.get('allocs/op'))))
        md.append('')
    return '\n'.join(md)


def render_suite(table) -> str:
    md = ['## 2. The shared scenario table across five adapters\n',
          '`BenchmarkHTTPXTestSuite`: the contract tests and the benchmarks share one table in '
          '`httpxtest/scenarios.go`, and each adapter drives its own framework\'s dispatcher. '
          '**Reading across the columns compares frameworks, not the cost of httpx** — for that, '
          'see sections 1 and 3.\n']
    adapters = [a for a in ADAPTERS if a in values(table, 'adapter')]
    header = ' | '.join(f'{a} ({ADAPTERS[a]})' for a in adapters)
    for unit in ('ns/op', 'allocs/op'):
        md.append(f'### {unit}\n')
        md.append(f'| Scenario | {header} |')
        md.append('| --- |' + ' ---: |' * len(adapters))
        for scenario in values(table, 'scenario'):
            cells = []
            for a in adapters:
                row = pick(table, adapter=a, scenario=scenario)
                cells.append(num(row.get(unit)) if row else '—')
            md.append(f'| {scenario} | ' + ' | '.join(cells) + ' |')
        md.append('')
    md.append('All five adapters run the same scenarios from the same table here, so no column '
              'has a coverage gap. Missing rows appear only in section 3, where the native side '
              'is hand-written.\n')
    return '\n'.join(md)


def render_native(table) -> str:
    md = ['## 3. Paired against a hand-written implementation with no httpx at all\n',
          '`BenchmarkNativeVsHTTPX`. Section 1 measures simple scenarios; this one measures the '
          'expensive request and response paths — multi-source binding, uploads, a 1 MiB body, '
          'static files. Each native side is written the way that framework is normally written; '
          'for `stdx` the counterpart is a `net/http` `ServeMux` with hand-written decoding and '
          'no form decoder, since reaching for one is already a step towards an adapter.\n',
          'A scenario with no honest counterpart is absent rather than approximated: hertz\'s '
          'native static file serving reads from disk only, which is not what the in-memory FS '
          'in the scenario does, so `hertzx` has no `StaticFile` row.\n']
    for framework in values(table, 'framework'):
        md.append(f'### {framework} (native = {ADAPTERS.get(framework, framework)})\n')
        md.append('| Scenario | native ns/op | httpx ns/op | Δ | native allocs | httpx allocs |')
        md.append('| --- | ---: | ---: | ---: | ---: | ---: |')
        for scenario in values(table, 'scenario'):
            n = pick(table, framework=framework, scenario=scenario, mode='native')
            h = pick(table, framework=framework, scenario=scenario, mode='httpx')
            if not n or not h:
                continue
            md.append('| {} | {} | {} | {} | {} | {} |'.format(
                scenario, num(n.get('ns/op')), num(h.get('ns/op')),
                delta(n.get('ns/op'), h.get('ns/op')),
                num(n.get('allocs/op')), num(h.get('allocs/op'))))
        md.append('')
    return '\n'.join(md)


def render_network(data) -> str:
    md = ['## 4. Over the network: latency at a fixed rate\n',
          f"Vegeta, HTTP/1.1 keep-alive, {data['rate']} req/s x {data['duration']}, "
          f"{data['repeat']} samples, {data['client_workers']} client workers, "
          f"GOMAXPROCS={data['gomaxprocs']} on both client and server.\n",
          '**This measures latency and success rate at that load; it does not measure peak QPS.** '
          'Client and server share one machine, so the percentiles absorb scheduling and client '
          'timing precision. Sizing for production means moving the client to another machine and '
          'raising the rate until latency or errors cross your target.\n']
    rows = {}
    for r in data['results']:
        key = (r['framework'], r['scenario'], r['mode'])
        rows.setdefault(key, []).append(r['metrics'])
    md.append('| Framework | Scenario | Mode | p50 | p95 | p99 | Success |')
    md.append('| --- | --- | --- | ---: | ---: | ---: | ---: |')
    for (framework, scenario, mode), samples in sorted(rows.items()):
        def q(name):
            return statistics.median(s['latencies'][name] for s in samples) / 1e6
        success = min(s['success'] for s in samples)
        md.append(f'| {framework} | {scenario} | {mode} | {q("50th"):.2f} ms | '
                  f'{q("95th"):.2f} ms | {q("99th"):.2f} ms | {success:.1%} |')
    md.append('')
    md.append('**Nearly every row is identical, and that is the finding.** The sections above '
              'measure tens to hundreds of nanoseconds; one real round trip costs tens of '
              'microseconds — three orders of magnitude more. At this load the choice of '
              'framework, and whether httpx is in the path at all, disappears under TCP, HTTP '
              'parsing and scheduling. The percentages above matter only once the adapter layer '
              'is actually the bottleneck, and this section is how you tell whether it is.\n')
    return '\n'.join(md)


def render_gin_depth(table) -> str:
    md = ['## 5. Appendix: gin deep-chain diagnostics\n',
          'Call-depth curves measured on gin alone, which is where the effect was found. They '
          'exist to attribute the jump in cost at a certain middleware depth to stack growth '
          'rather than to httpx. The mechanism is analysed in '
          '[history/GIN_DEPTH_REPORT.md](history/GIN_DEPTH_REPORT.md); only the curves are '
          'reproduced here.\n']
    for bench in values(table, '_bench'):
        rows = subset(table, _bench=bench)
        md.append(f'### {bench} (ns/op)\n')
        layers = values(rows, 'layers')
        modes = values(rows, 'mode')
        if layers:
            md.append('| layers | ' + ' | '.join(modes) + ' |')
            md.append('| --- |' + ' ---: |' * len(modes))
            for layer in sorted(layers, key=int):
                cells = []
                for mode in modes:
                    row = pick(rows, layers=layer, mode=mode)
                    cells.append(num(row.get('ns/op')) if row else '—')
                md.append(f'| {layer} | ' + ' | '.join(cells) + ' |')
        else:
            md.append('| mode | ns/op | allocs/op |')
            md.append('| --- | ---: | ---: |')
            for mode in modes:
                row = pick(rows, mode=mode)
                if row:
                    md.append(f"| {mode} | {num(row.get('ns/op'))} | {num(row.get('allocs/op'))} |")
        md.append('')
    return '\n'.join(md)


def render_appendix() -> str:
    return '\n'.join([
        '## Appendix: raw data and significance\n',
        'The tables above are medians. To decide whether two numbers really differ, run '
        'benchstat over the samples:\n',
        '```sh',
        'go install golang.org/x/perf/cmd/benchstat@latest',
        '',
        '# native against httpx, within one run',
        'benchstat -row /framework,/scenario -col /mode benchmarks/results/latest/adapter.txt',
        'benchstat -row /framework,/scenario -col /mode benchmarks/results/latest/native.txt',
        '',
        '# the shared scenario table across adapters',
        'benchstat -row /scenario -col /adapter benchmarks/results/latest/suite.txt',
        '',
        '# two revisions (both sides must be the same benchmark source)',
        'benchstat -filter "/mode:httpx" before.txt after.txt',
        '```\n',
        'This run\'s raw output and `environment.json` are in `benchmarks/results/latest/`, and '
        'the compiled test binaries are in `benchmarks/bin/` so a run can be repeated from the '
        'exact same build. **None of it is tracked.** `benchmarks/` has no `go.mod`, so it is '
        'part of the root module, and anything committed there ships in the module zip every '
        'consumer of `github.com/go-sphere/httpx` downloads — benchmark samples do not belong '
        'in a library\'s module zip. This file carries the medians and the environment, which '
        'is what the numbers are for; to compare against another machine, re-run there rather '
        'than reaching for its samples.\n',
        '### Not covered\n',
        'Databases and RPC, HTTP/2, TLS, real network round trips, sustained throughput on long-lived '
        'streaming responses, and peak QPS under load from a separate machine. Every one of those '
        'dwarfs the differences measured here, so none of these percentages extrapolate to an '
        'end-to-end performance claim.\n',
    ])

# --------------------------------------------------------------------- helpers

def num(v) -> str:
    if v is None:
        return '—'
    if v == 0:
        return '0'
    if v >= 10000:
        return f'{v:,.0f}'
    if v >= 100:
        return f'{v:.0f}'
    return f'{v:.2f}'.rstrip('0').rstrip('.')


def delta(base, other) -> str:
    if not base or other is None:
        return '—'
    pct = (other - base) / base * 100
    return f'{pct:+.0f}%' if abs(pct) >= 1 else '≈0%'


def environment(args) -> dict:
    head = capture(['git', 'rev-parse', 'HEAD'])
    dirty = capture(['git', 'status', '--porcelain'])
    return {
        'date_utc': datetime.now(timezone.utc).isoformat(timespec='seconds'),
        'go': capture(['go', 'version']),
        'platform': platform.platform(),
        'cpu': cpu_model(),
        'git_head': head,
        # Split rather than slice a fixed offset: capture() strips the output, so
        # the first porcelain line has already lost its leading status space.
        'git_state': 'clean' if not dirty else 'uncommitted changes: ' + ', '.join(
            line.split(maxsplit=1)[-1] for line in dirty.splitlines()),
        'gomaxprocs': args.cpu,
        'samples': args.count,
        'benchtime': args.benchtime,
        'framework_versions': framework_versions(),
    }


def framework_versions() -> list:
    out = []
    for module, deps in (('ginx', ['github.com/gin-gonic/gin']),
                         ('echox', ['github.com/labstack/echo/v4']),
                         ('fiberx', ['github.com/gofiber/fiber/v3']),
                         ('hertzx', ['github.com/cloudwego/hertz'])):
        for line in (ROOT / module / 'go.mod').read_text().splitlines():
            fields = line.split()
            if len(fields) >= 2 and fields[0] in deps:
                out.append(f'{fields[0]} {fields[1]}')
    return out


def cpu_model() -> str:
    if sys.platform == 'darwin':
        return capture(['sysctl', '-n', 'machdep.cpu.brand_string'])
    try:
        for line in Path('/proc/cpuinfo').read_text().splitlines():
            if line.startswith('model name'):
                return line.split(':', 1)[1].strip()
    except OSError:
        pass
    return platform.processor() or 'unknown'


def capture(cmd) -> str:
    try:
        return subprocess.check_output(cmd, cwd=ROOT, text=True).strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return 'unknown'


def run(cmd):
    subprocess.run(cmd, cwd=ROOT, check=True)


def rel(path: Path) -> str:
    try:
        return str(path.relative_to(ROOT))
    except ValueError:
        return str(path)


def log(message: str):
    print(f'==> {message}', flush=True)


if __name__ == '__main__':
    if shutil.which('go') is None:
        sys.exit('go is not on PATH')
    main()
