package registry

// GC removes retired modules with zero active references (gap #2).
func (r *Registry) GC() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for name, versions := range r.modules {
		kept := versions[:0]
		for _, m := range versions {
			if m.State == StateRetired && r.refs[m.key()] == 0 {
				removed++
				continue
			}
			kept = append(kept, m)
		}
		r.modules[name] = kept
	}
	return removed
}
