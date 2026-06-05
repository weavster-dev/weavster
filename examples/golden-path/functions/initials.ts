// Custom transform (escape hatch): pure JSON in, JSON out — stays portable to
// the production WASM runtime. Adds an uppercased initials field.
interface Order {
  first?: string;
  last?: string;
  [key: string]: unknown;
}

export default (order: Order): Order => ({
  ...order,
  initials: `${order.first?.[0] ?? ''}${order.last?.[0] ?? ''}`.toUpperCase(),
});
