// Command weavster is the single static binary that hosts the Weavster
// message-oriented integration platform: API gateway, scheduler, executor,
// state manager, adapters, and CLI shell (composition root).
package main

func main() {
	if err := run(); err != nil {
		panic(err) // replaced by proper exit semantics in the CLI phase (P6)
	}
}

// run is the composition root. It is fleshed out during the build phases.
func run() error {
	return nil
}
