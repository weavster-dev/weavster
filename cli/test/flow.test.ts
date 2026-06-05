import { describe, expect, it } from 'vitest';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { checkFlows, listFlows, loadFlow } from '../src/flow.js';

const here = dirname(fileURLToPath(import.meta.url));
const goldenPath = resolve(here, '../../examples/golden-path');
const harness = resolve(here, '../../tests/fixtures/harness');

describe('loadFlow', () => {
  it('loads and validates a real flow', () => {
    const { flow, errors } = loadFlow(goldenPath, 'order');
    expect(errors).toEqual([]);
    expect(flow?.steps[0]).toEqual({ op: 'str', fn: 'upper', from: 'id' });
  });

  it('reports a missing flow', () => {
    const { flow, errors } = loadFlow(goldenPath, 'nope');
    expect(flow).toBeNull();
    expect(errors.join('\n')).toMatch(/no flow "nope"/);
  });
});

describe('listFlows', () => {
  it('lists flow names without extension', () => {
    expect(listFlows(goldenPath)).toEqual(['order']);
  });

  it('returns empty when there is no flows directory', () => {
    expect(listFlows(resolve(harness, 'badflow'))).toEqual([]);
  });
});

describe('checkFlows', () => {
  it('reports each flow as valid', () => {
    expect(checkFlows(goldenPath)).toEqual([{ file: 'flows/order.yaml', ok: true, errors: [] }]);
  });
});
