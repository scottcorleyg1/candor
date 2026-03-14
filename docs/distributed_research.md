# Research Note: Distributed Semantic Networking

This note explores how Candor can bridge the gap between low-level systems programming and high-level datacenter orchestration.

## 1. The "Ambiguity Trap" in Legacy Distribution
In current environments (Go, Rust, C++), a distributed call is just bytes over a wire (TCP/UDP). The **Intent** and **Side-Effect Constraints** are lost at the network boundary.
- **Problem**: An AI agent cannot guarantee that a remote worker will adhere to the safety constraints defined in the local source code.
- **Risk**: "Invisible" side effects in remote workers.

## 2. The Candor Model: Capability-Based RPC
Candor's networking layer should treat a network connection not just as a stream, but as a **Contractual Tunnel**.

### A. Semantic Serialization
When a Candor function is called across a network:
1.  The **Arguments** are serialized (standard).
2.  The **Effect Registry** (permissions) is serialized.
3.  The **Requires Contracts** are verified on the remote machine before execution.

### B. Use Case: AI Cluster Orchestration
An AI can manage 1,000 servers by simply calling:
```candor
spawn_on(server_list) {
    requires effects(fs_read)
    requires no_effects(net_out)
    do_heavy_lifting(data)
}
```
If any server in the `server_list` fails to provide an environment that supports these constraints, the code fails to run.

## 3. Efficiency Gains
- **Fiscal**: Smaller code (no manual validation logic) = Fewer tokens = Lower API costs.
- **Energy**: Explicit `pure` annotations allow the runtime to bypass expensive cache-coherency protocols across the network when data hasn't changed.
- **Scalability**: By making networking "honest," we eliminate the need for massive, complex "Security Middleware" layers, reducing latency.

---
*Date: 2026-03-14*
*Author: Scott W. Corley / AI Assistant*
