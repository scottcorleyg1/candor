# Welcome to Candor

Welcome to the world of Candor! 

Candor is a modern, systems-oriented programming language designed for developers who value clarity, safety, and performance. Whether you're building a low-level system or a high-performance tool, Candor provides the power you need without compromising on readability.

## Why Candor?

- **Safety First**: Strong, static typing with advanced ownership semantics ensures your code is safe from common memory errors.
- **Expressive Syntax**: Candor's syntax is designed to be familiar yet powerful, making it easy to learn and write.
- **Seamless Performance**: Compiles to readable C11, giving you the performance of C with the safety of a modern language.
- **Bootstrapped Maturity**: The Candor compiler is written in Candor itself, proving its capability and stability.

## Getting Started

### 1. Prerequisites
To use Candor, you'll need:
- A C11-compliant C compiler (like `gcc` or `clang`) installed on your system.
- The Candor compiler binary (`candor`).

### 2. Your First Program
Create a file named `hello.cnd`:
```candor
fn main() {
    print("Hello, Candor world!");
}
```

### 3. Compiling and Running
Use the Candor driver to compile your program:
```bash
candor hello.cnd -o hello
./hello
```

## Explorations

- **Check out the `examples/` directory**: See how the typechecker and other tools are implemented.
- **Read "What is in Core"**: Formal documentation on the language's v0.1 features.
- **View the Roadmap**: See what's coming next for Candor.

## Joining the Community

We're excited to have you with us! Candor is an evolving project, and your feedback, contributions, and ideas are what will help it grow.

- **Found a bug?** Open an issue on our repository.
- **Have an idea?** Start a discussion or submit a pull request.

Happy coding with Candor!
