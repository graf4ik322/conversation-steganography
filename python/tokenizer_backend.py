#!/usr/bin/env python3
"""Lightweight tokenizer-only backend for OpenRouterModel.

Loads ONLY the tokenizer (not the model) from HuggingFace.
Much lighter than hf_model.py — runs on any Python with tokenizers installed.

Protocol: line-delimited JSON (same as hf_model.py)
  Request:  {"op": "tokenize", "text": "hello world"}
  Response: {"ok": true, "tokens": [123, 456]}

  Request:  {"op": "detokenize", "tokens": [123, 456]}
  Response: {"ok": true, "text": "hello world"}

  Request:  {"op": "tokens_to_ids", "tokens": ["hello", "world"]}
  Response: {"ok": true, "ids": [31373, 1293]}
"""

import argparse
import hashlib
import json
import sys


def reply(**values):
    print(json.dumps({"ok": True, **values}, separators=(",", ":")), flush=True)


def fail(exc):
    print(json.dumps({"ok": False, "error": str(exc)}, separators=(",", ":")), flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", required=True)
    parser.add_argument("--revision", default="main")
    args = parser.parse_args()

    from transformers import AutoTokenizer

    hf_tokenizer = AutoTokenizer.from_pretrained(args.model, revision=args.revision)
    fingerprint = hashlib.sha256(
        f"{args.model}:{args.revision}:{hf_tokenizer.vocab_size}".encode()
    ).hexdigest()[:16]

    if hf_tokenizer.pad_token is None:
        hf_tokenizer.pad_token = hf_tokenizer.eos_token

    reply(fingerprint=fingerprint)

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError as e:
            fail(f"invalid JSON: {e}")
            continue

        op = req.get("op")
        try:
            if op == "info":
                reply(fingerprint=fingerprint)
            elif op == "tokenize":
                text = req.get("text", "")
                ids = hf_tokenizer.encode(text)
                reply(tokens=ids)
            elif op == "detokenize":
                ids = req.get("tokens", [])
                text = hf_tokenizer.decode(ids, skip_special_tokens=True)
                reply(text=text)
            elif op == "tokens_to_ids":
                tokens = req.get("tokens", [])
                ids = hf_tokenizer.convert_tokens_to_ids(tokens)
                reply(ids=ids)
            else:
                fail(f"unknown operation: {op}")
        except Exception as e:
            fail(f"{op} failed: {e}")


if __name__ == "__main__":
    main()
