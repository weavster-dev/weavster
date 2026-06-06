import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';

/** A pipeline input: produces the raw text to parse. */
export interface Source {
  read(): Promise<string>;
}

/** A pipeline output: consumes the serialized text. */
export interface Sink {
  write(text: string): Promise<void>;
}

export function fileSource(path: string): Source {
  return {
    async read() {
      if (!existsSync(path)) throw new Error(`no input file "${path}"`);
      return readFileSync(path, 'utf8');
    },
  };
}

export function stdinSource(): Source {
  return {
    async read() {
      const chunks: Buffer[] = [];
      for await (const chunk of process.stdin) chunks.push(chunk as Buffer);
      return Buffer.concat(chunks).toString('utf8');
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
