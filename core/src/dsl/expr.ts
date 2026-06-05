/**
 * Expression evaluator for the v0alpha2 DSL.
 *
 * A value position is an expression: a literal, a `$path` reference, or a
 * single-key `{ _op: ... }` operator. Plain objects and arrays are evaluated
 * deeply, so references and operators can be nested anywhere inside a value.
 *
 * Sigils: `$path` reads from the working document; `$$x` is the literal `$x`;
 * a single `_`-prefixed key invokes an operator. `_lit` returns its argument
 * verbatim (the escape for values that would otherwise look like operators or
 * path references).
 */
import type { Document } from '../model.js';
import { getValue } from '../path.js';
import { TransformError } from './errors.js';

export interface Ctx {
  working: Document;
}

export type ValueOp = (arg: unknown, ctx: Ctx) => unknown;

/** Value operators (`_concat`, comparisons, …). Populated as the DSL grows. */
export const VALUE_OPS: Record<string, ValueOp> = {};

function isOperator(keys: string[]): boolean {
  return keys.length === 1 && keys[0].startsWith('_');
}

/** Evaluate an expression against the working document, returning a JS value. */
export function evalExpr(expr: unknown, ctx: Ctx): unknown {
  if (typeof expr === 'string') {
    if (expr.startsWith('$$')) return expr.slice(1);
    if (expr.startsWith('$')) return getValue(ctx.working, expr.slice(1));
    return expr;
  }

  if (Array.isArray(expr)) return expr.map((item) => evalExpr(item, ctx));

  if (expr !== null && typeof expr === 'object') {
    const record = expr as Record<string, unknown>;
    const keys = Object.keys(record);
    if (isOperator(keys)) {
      const op = keys[0];
      if (op === '_lit') return record._lit;
      const impl = VALUE_OPS[op];
      if (impl === undefined) throw new TransformError(`unknown operator "${op}"`);
      return impl(record[op], ctx);
    }
    const out: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(record)) out[key] = evalExpr(value, ctx);
    return out;
  }

  return expr;
}
