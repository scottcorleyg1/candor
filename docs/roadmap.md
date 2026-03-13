# Candor Project Roadmap

This document outlines the planned trajectory for Candor beyond the v0.1 release. Our goal is to build a high-performance, safe, and ergonomic systems language.

## Phase 1: Stability & Tooling (Immediate Post-Release)
- **Standard Library Development**: Implement core modules for `io`, `path`, `time`, and `random`.
- **LSP (Language Server Protocol)**: Build a basic LSP server to provide syntax highlighting, go-to-definition, and autocomplete in VS Code and other editors.
- **Improved Error Messaging**: Refine the typechecker to provide more helpful, diagnostic-style error messages with code snippets and suggestions.

## Phase 2: Performance & Backends
- **LLVM Backend**: Move beyond C emission to a native LLVM backend for better optimizations and faster build times.
- **Memory Management Refinement**: Explore further optimizations for reference counting and explicit ownership to minimize runtime overhead.
- **Parallelism & Concurrency**: Design and implement safe concurrency primitives (e.g., channels, async/await) that integrate with Candor's ownership model.

## Phase 3: Ecosystem & Verification
- **Package Manager**: Develop a simple, robust package manager for sharing and managing dependencies.
- **Advanced Formal Verification**: Expand the contract system to include compile-time verification of preconditions and postconditions where possible.
- **Interoperability**: Enhance C interoperability to allow seamless integration with existing C/C++ libraries.

## Future Exploration
- **WebAssembly (Wasm) Support**: Target WebAssembly for high-performance web applications.
- **Cross-Compilation**: Streamline the process for targeting multiple OS and architectures.
- **Metaprogramming**: Investigate safe macros or compile-time reflection capabilities.

---
*Candor is an open-source project. Community feedback and contributions are always welcome as we move through these phases.*
