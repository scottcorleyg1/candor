#!/usr/bin/env python3
"""
Candor Corpus Token Benchmark

Measures real Claude token counts for whole Candor programs in both
Verification Form (full syntax) and Agent Form (compact signatures).
Reports per-file savings and mean ± std across the corpus.

Usage:
    python benchmarks/tokenizer/corpus_benchmark.py
    python benchmarks/tokenizer/corpus_benchmark.py --model claude-opus-4-7

Requires: pip install anthropic
          ANTHROPIC_API_KEY environment variable set

Statistical claim target:
  Current published numbers (60% function-level, 83% per ? site) are based
  on constructed examples. This script validates them against real programs
  to produce mean ± std for a postable claim.
"""

import argparse
import json
import math
import os
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import anthropic

REPO_ROOT = Path(__file__).parent.parent.parent
EXAMPLES_DIR = REPO_ROOT / "examples"
AGENT_FORM_DIR = EXAMPLES_DIR / "agent_form"
RESULTS_DIR = Path(__file__).parent / "results"

CORPUS_FILES = ["log_filter.cnd", "word_stats.cnd", "config.cnd", "pipeline.cnd"]


def count_tokens(client: anthropic.Anthropic, model: str, text: str, baseline: int) -> int:
    resp = client.messages.count_tokens(
        model=model,
        messages=[{"role": "user", "content": "X " + text}]
    )
    return resp.input_tokens - baseline


def get_baseline(client: anthropic.Anthropic, model: str) -> int:
    resp = client.messages.count_tokens(
        model=model,
        messages=[{"role": "user", "content": "X"}]
    )
    return resp.input_tokens


def mean_std(values: list[float]) -> tuple[float, float]:
    if len(values) < 2:
        return (values[0] if values else 0.0), 0.0
    m = sum(values) / len(values)
    variance = sum((x - m) ** 2 for x in values) / (len(values) - 1)
    return m, math.sqrt(variance)


def run_corpus_benchmark(model: str, agent_dir: Path) -> dict:
    client = anthropic.Anthropic()

    print(f"Model: {model}")
    print(f"Agent form dir: {agent_dir.name}")
    print("Establishing baseline...", flush=True)
    baseline = get_baseline(client, model)
    print(f"Baseline overhead: {baseline} tokens\n")

    results = {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "model": model,
        "agent_dir": agent_dir.name,
        "baseline_overhead": baseline,
        "files": {}
    }

    savings_pcts = []

    for fname in CORPUS_FILES:
        label = fname.replace(".cnd", "")
        vf_path = EXAMPLES_DIR / fname
        af_path = agent_dir / fname

        if not vf_path.exists():
            print(f"  SKIP {label}: {vf_path} not found")
            continue
        if not af_path.exists():
            print(f"  SKIP {label}: {af_path} not found")
            continue

        vf_text = vf_path.read_text(encoding="utf-8")
        af_text = af_path.read_text(encoding="utf-8")

        print(f"  Measuring {label}...", end="", flush=True)
        vf_tok = count_tokens(client, model, vf_text, baseline)
        time.sleep(0.1)
        af_tok = count_tokens(client, model, af_text, baseline)
        time.sleep(0.1)

        saved = vf_tok - af_tok
        pct = round(saved / vf_tok * 100, 1) if vf_tok > 0 else 0.0
        savings_pcts.append(pct)

        results["files"][label] = {
            "verification_tokens": vf_tok,
            "agent_tokens": af_tok,
            "tokens_saved": saved,
            "savings_pct": pct,
        }
        print(f" {vf_tok} -> {af_tok} tok  ({pct}% savings)")

    if savings_pcts:
        m, s = mean_std(savings_pcts)
        results["summary"] = {
            "n_files": len(savings_pcts),
            "mean_savings_pct": round(m, 1),
            "std_savings_pct": round(s, 1),
            "min_savings_pct": round(min(savings_pcts), 1),
            "max_savings_pct": round(max(savings_pcts), 1),
        }

    return results


def print_report(results: dict):
    model = results["model"]
    ts = results["timestamp"]
    print(f"\n{'='*65}")
    print(f"Candor Corpus Token Benchmark")
    print(f"Model: {model}   {ts[:19]}Z")
    print(f"{'='*65}\n")

    print(f"{'File':<18} {'Verif.':>8} {'Agent':>8} {'Saved':>7} {'Savings':>9}")
    print(f"{'-'*18} {'-'*8} {'-'*8} {'-'*7} {'-'*9}")
    for label, d in results["files"].items():
        print(f"{label:<18} {d['verification_tokens']:>8} {d['agent_tokens']:>8} "
              f"{d['tokens_saved']:>7} {d['savings_pct']:>8.1f}%")

    if "summary" in results:
        s = results["summary"]
        print(f"\nSummary across {s['n_files']} programs:")
        print(f"  Mean savings:  {s['mean_savings_pct']:.1f}% +/- {s['std_savings_pct']:.1f}%")
        print(f"  Range:         {s['min_savings_pct']:.1f}% - {s['max_savings_pct']:.1f}%")
        print()
        print("Interpretation:")
        print(f"  An AI writing these programs in Agent Form consumes on average")
        print(f"  {s['mean_savings_pct']:.0f}% fewer tokens than in Verification Form.")
        print(f"  Same program. Same semantics. Same compiled output.")


def main():
    parser = argparse.ArgumentParser(description="Candor corpus token benchmark")
    parser.add_argument("--model", default="claude-sonnet-4-6",
                        help="Model to benchmark against (default: claude-sonnet-4-6)")
    parser.add_argument("--agent-dir", default="agent_form",
                        help="Subdirectory of examples/ containing agent form files (default: agent_form)")
    parser.add_argument("--save", action="store_true",
                        help="Save results to results/ directory")
    args = parser.parse_args()

    if not os.environ.get("ANTHROPIC_API_KEY"):
        print("Error: ANTHROPIC_API_KEY not set", file=sys.stderr)
        sys.exit(1)

    agent_dir = EXAMPLES_DIR / args.agent_dir
    results = run_corpus_benchmark(args.model, agent_dir)
    print_report(results)

    if args.save:
        RESULTS_DIR.mkdir(exist_ok=True)
        ts = datetime.now().strftime("%Y-%m-%d")
        model_slug = args.model.replace("/", "-")
        dir_slug = args.agent_dir.replace("/", "-").replace("_", "-")
        out_path = RESULTS_DIR / f"{ts}_corpus_{dir_slug}_{model_slug}.json"
        out_path.write_text(json.dumps(results, indent=2), encoding="utf-8")
        print(f"\nResults saved to {out_path.relative_to(REPO_ROOT)}")


if __name__ == "__main__":
    main()
