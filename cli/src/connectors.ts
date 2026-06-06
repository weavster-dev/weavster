import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { createInterface } from 'node:readline';

/** A pipeline input: yields a stream of raw document texts (once for a file, many for a stream). */
export interface Source {
  documents(): AsyncIterable<string>;
}

/** A pipeline output: consumes each serialized document. */
export interface Sink {
  write(text: string): Promise<void>;
}

export function fileSource(path: string): Source {
  return {
    async *documents() {
      if (!existsSync(path)) throw new Error(`no input file "${path}"`);
      yield readFileSync(path, 'utf8');
    },
  };
}

export function stdinSource(): Source {
  return {
    async *documents() {
      // Line-delimited: each non-empty line is one document, yielded as it arrives.
      const lines = createInterface({ input: process.stdin, crlfDelay: Number.POSITIVE_INFINITY });
      for await (const line of lines) {
        const text = line.trim();
        if (text) yield text;
      }
    },
  };
}

export function fileSink(path: string): Sink {
  return {
    async write(text) {
      mkdirSync(dirname(path), { recursive: true });
      writeFileSync(path, text);
    },
  };
}

export function stdoutSink(): Sink {
  return {
    async write(text) {
      process.stdout.write(text);
    },
  };
}
