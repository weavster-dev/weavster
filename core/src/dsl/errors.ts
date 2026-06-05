/** Thrown when a step or expression is malformed or references a bad path. */
export class TransformError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'TransformError';
  }
}
