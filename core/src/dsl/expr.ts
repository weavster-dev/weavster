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

const toStr = (value: unknown): string =>
  value === null || value === undefined ? '' : String(value);
const deepEqual = (a: unknown, b: unknown): boolean => JSON.stringify(a) === JSON.stringify(b);

function pair(arg: unknown, op: string): [unknown, unknown] {
  if (!Array.isArray(arg) || arg.length !== 2) {
    throw new TransformError(`"${op}" expects a list of two expressions`);
  }
  return [arg[0], arg[1]];
}

function list(arg: unknown, op: string): unknown[] {
  if (!Array.isArray(arg)) throw new TransformError(`"${op}" expects a list`);
  return arg;
}

function record(arg: unknown, op: string): Record<string, unknown> {
  if (arg === null || typeof arg !== 'object' || Array.isArray(arg)) {
    throw new TransformError(`"${op}" expects a map`);
  }
  return arg as Record<string, unknown>;
}

/** Value operators usable in any value position. */
export const VALUE_OPS: Record<string, ValueOp> = {
  _concat(arg, ctx) {
    const parts = Array.isArray(arg) ? arg : list(record(arg, '_concat').parts, '_concat');
    const sep = Array.isArray(arg) ? '' : toStr(record(arg, '_concat').sep);
    return parts.map((p) => toStr(evalExpr(p, ctx))).join(sep);
  },
  _upper: (arg, ctx) => toStr(evalExpr(arg, ctx)).toUpperCase(),
  _lower: (arg, ctx) => toStr(evalExpr(arg, ctx)).toLowerCase(),
  _trim: (arg, ctx) => toStr(evalExpr(arg, ctx)).trim(),
  _toIso(arg, ctx) {
    const date = new Date(toStr(evalExpr(arg, ctx)));
    if (Number.isNaN(date.getTime())) throw new TransformError('"_toIso" got an unparseable date');
    return date.toISOString();
  },
  _coalesce(arg, ctx) {
    for (const expr of list(arg, '_coalesce')) {
      const value = evalExpr(expr, ctx);
      if (value !== null && value !== undefined) return value;
    }
    return null;
  },
  _eq(arg, ctx) {
    const [a, b] = pair(arg, '_eq');
    return deepEqual(evalExpr(a, ctx), evalExpr(b, ctx));
  },
  _exists: (arg, ctx) => evalExpr(arg, ctx) !== undefined,
  _gt(arg, ctx) {
    const [a, b] = pair(arg, '_gt');
    return (evalExpr(a, ctx) as number) > (evalExpr(b, ctx) as number);
  },
  _lt(arg, ctx) {
    const [a, b] = pair(arg, '_lt');
    return (evalExpr(a, ctx) as number) < (evalExpr(b, ctx) as number);
  },
  _in(arg, ctx) {
    const [needle, haystack] = pair(arg, '_in');
    const items = evalExpr(haystack, ctx);
    if (!Array.isArray(items))
      throw new TransformError('"_in" expects an array as its second argument');
    const value = evalExpr(needle, ctx);
    return items.some((item) => deepEqual(item, value));
  },
  _and: (arg, ctx) => list(arg, '_and').every((e) => Boolean(evalExpr(e, ctx))),
  _or: (arg, ctx) => list(arg, '_or').some((e) => Boolean(evalExpr(e, ctx))),
  _not: (arg, ctx) => !evalExpr(arg, ctx),
  _cond(arg, ctx) {
    const o = record(arg, '_cond');
    return Boolean(evalExpr(o.if, ctx)) ? evalExpr(o.then, ctx) : evalExpr(o.else, ctx);
  },
};

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
