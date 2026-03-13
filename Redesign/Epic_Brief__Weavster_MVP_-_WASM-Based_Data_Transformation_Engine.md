# Epic Brief: Weavster MVP - WASM-Based Data Transformation Engine

## Summary

Weavster is a modern, developer-friendly alternative to legacy enterprise integration engines like Cloverleaf, InterSystems Ensemble, and traditional ESB tools. This MVP validates the core technical architecture: a WASM-based transformation engine that processes files (CSV, JSON, XML, HL7, EDI, X12) with declarative YAML configuration, comprehensive testing capabilities, and single-binary distribution. The goal is to prove that WASM compilation is viable for data transformations, establish a foundation for the full product vision, and demonstrate a better developer experience for the "dirty, not glorious work of interoperability."

## Context & Problem

### Who's Affected

**Primary Users:** Integration engineers, healthcare IT professionals, and developers working on enterprise data interoperability. These are the people dealing with the unglamorous but critical work of moving and transforming data between systems—processing HL7 messages, EDI transactions, CSV extracts, and various file formats in both batch and real-time scenarios.

**Current Pain Points:**

1. **Repetitive Code Instead of Config** - Integration engineers write the same transformation logic over and over in custom code rather than using declarative configuration. Every field mapping, every filter, every data cleanup requires writing and maintaining code.

2. **No Testing Story** - There's no good way to validate transformations work correctly before deploying to production. Engineers resort to manual testing, hoping their transforms handle edge cases properly.

3. **Expensive & Hard-to-Use Tools** - Traditional ESB tools are either:
   - Point-and-click "no-code" interfaces that are clunky and hard to version control
   - Enterprise platforms that are prohibitively expensive for small organizations
   - Documentation is often gatekept behind paywalls or vendor support contracts

4. **Deployment Complexity** - Existing tools require complex setup, multiple dependencies, and are difficult to run locally for development and testing.

### The Opportunity

Modern data engineers have tools like dbt that provide declarative configuration, version control, and comprehensive testing for batch analytics. But for real-time integration and file processing—especially in healthcare and enterprise contexts—engineers are stuck with legacy tools from the 2000s.

Weavster brings the dbt experience to integration engineering: YAML-based configuration, git-friendly workflows, local development with a single binary, and a testing framework that lets engineers validate their transformations before deployment.

### Why This MVP Matters

This MVP is **internal validation** to prove three critical technical decisions:

1. **WASM Compilation Works** - Demonstrate that declarative YAML transforms can be compiled to WASM and executed efficiently, enabling:
   - Security isolation (sandboxed execution)
   - Portability (run anywhere: edge, cloud, embedded)
   - Future extensibility (users can write custom transforms in any WASM-compatible language)
   - Single binary distribution (no Cargo or Rust toolchain required on user machines)

2. **Architecture is Sound** - Validate that the core components (file watching, glob patterns, transform pipeline, testing framework) integrate cleanly and provide a solid foundation for the full product vision.

3. **Developer Experience is Right** - Prove that the declarative YAML approach, combined with a testing framework, actually solves the pain points better than existing tools.

### Deployment Context

**For this MVP:**
- Local development mode: Engineers run `weavster` on their laptops to develop and test transformations

**Future Vision:**
- Server mode: Small organizations deploy a single binary for production workloads
- Distributed mode: Enterprise organizations deploy to Kubernetes for high-availability, scalable processing

### Success Criteria for MVP

✅ **Technical Proof:** WASM compilation pipeline works end-to-end (YAML → compiled WASM → execution)

✅ **Functional Validation:** Core transforms (map, drop, add_fields, filter) work correctly with file watching and glob patterns

✅ **Testing Framework:** Users can define test cases in YAML with input/expected output and validate their flows

✅ **Foundation Quality:** Codebase is clean, well-tested, and ready to build upon for the full product vision

### Out of Scope for MVP

- Additional transforms (regex, template, lookup, coalesce) - deferred to future iterations
- Kafka, HTTP, PostgreSQL connectors - file-based only for MVP
- Production deployment tooling - focus on local development
- Advanced error handling and observability - minimal logging is sufficient
- Multi-format support beyond JSONL - start simple, add CSV/XML/HL7 later

---

## Key Assumptions

1. **WASM runtime choice:** We'll use Wasmtime (better Rust integration, mature ecosystem)
2. **File format:** JSONL only for MVP (easiest to parse and validate)
3. **Transform execution:** Compile YAML to WASM at flow load time (not pre-compiled)
4. **Testing approach:** YAML test definitions with simple equality assertions
5. **File watching:** Use `notify` crate for filesystem monitoring
6. **Post-processing:** Configurable in YAML (move/delete/leave files) with "leave" as default
