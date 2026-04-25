# GeoAddr — Geographic Internet Addressing Specification
### Working Draft v0.1 — 2026-04-17
### Status: Exploratory Spec — Pre-RFC

---

## Abstract

GeoAddr defines a new internet addressing system designed from three principles the existing systems violate:

1. **Human-first** — an address must be easy to say, write, and remember
2. **Geographically meaningful** — the address itself tells you where the endpoint is
3. **System-agnostic** — the core spec makes no assumptions about the underlying network; existing and future systems attach via addendums

IPv4 ran out of space. IPv6 solved quantity but destroyed usability. GeoAddr starts over with humans as the primary design constraint and geography as the organizing principle.

### Abstract — Extended

Each of the three principles is a deliberate rejection of a specific failure in existing systems.

**On Human-first:**
Every addressing system ever built has been machine-first. IPv4 addresses are 32-bit binary numbers that humans were taught to read as four decimal octets — as an afterthought. IPv6 is 128-bit hexadecimal: the designers gave up on humans entirely and assumed software would handle display. GeoAddr flips this. The format `262.060.4821` is the native format, not a display layer over something more "real." What humans see IS the address. If a human cannot say it aloud, write it from memory, or recognize it on sight, the format is non-conforming — not just inconvenient, but a spec violation.

**On Geographically meaningful:**
If you see the address `203.0.113.42` you know nothing about where that device is. Geolocation databases (MaxMind and others) exist entirely to compensate for this — they are large commercial businesses built on a fundamental design flaw in IPv4. In GeoAddr, `262.060.4821` immediately communicates: western hemisphere, mid-latitudes — Texas-ish. The address is its own geography. No external lookup table is needed to understand the rough physical context of an endpoint. The meaning is in the format by construction, not by convention.

**On System-agnostic:**
IPv4 was designed for ARPANET. IPv6 was designed to replace IPv4. Both assume a specific network architecture. Both will eventually be obsolete. GeoAddr assumes nothing about the network beneath it. It defines a coordinate system and an address format. Whether packets travel over Ethernet, 6G cellular, satellite mesh, or a network technology that does not yet exist is an addendum problem — not a core problem. The address format is designed to outlive any specific network technology by decades.

**The thesis:**
The final sentence of the abstract — *"GeoAddr starts over with humans as the primary design constraint and geography as the organizing principle"* — is the test every design decision in this spec must pass. If a proposed feature or addendum serves neither humans nor geography, it does not belong in GeoAddr. It may belong in a system-specific addendum, but not in the core.

---

## 1. The Problem

Internet addressing has two unsolved problems that are distinct but always treated as one:

**Problem A — Quantity.** IPv4's 32-bit space (~4.3 billion addresses) is exhausted. IPv6 solved this with 128 bits but at the cost of human usability.

**Problem B — Meaning.** No existing address format tells you anything about the endpoint. `192.168.1.1` and `203.0.113.42` are equally opaque. Geolocation is a lookup-table hack bolted on after the fact.

GeoAddr solves both by making geography the address structure itself.

---

## 2. Core Concepts

### 2.1 The Orange Slice Model

The Earth is divided into a coordinate grid using two orthogonal axes:

- **Longitude axis** (0–360) — slices from pole to pole, like sections of an orange. 0 begins at the prime meridian, increasing eastward.
- **Latitude axis** (0–180) — horizontal bands at a right angle to the longitude slices. 0 = north pole, 90 = equator, 180 = south pole.

Every point on Earth maps to a unique pair of coordinates, both values 360 or less. This is the **geographic cell** — the foundational unit of a GeoAddr address.

### 2.2 Address Format

```
[longitude].[latitude].[local-id]
```

**Examples:**
```
262.060.4821        Austin, Texas, device 4821
002.041.0093        London, England, device 93
139.035.7712        Tokyo, Japan, device 7712
```

Three groups. Numeric only. Pronounceable. Writable on a napkin.

**Spoken:** "two-six-two, zero-six-zero, four-eight-two-one"

### 2.3 Precision

Whole-degree coordinates define regional cells (~111km × 111km at the equator). Decimal precision narrows the cell:

| Precision | Cell size (approx) | Use |
|---|---|---|
| 1 degree | ~111 km | Regional authority boundary |
| 0.1 degree | ~11 km | City / metro registry |
| 0.01 degree | ~1.1 km | Neighborhood / building |
| 0.001 degree | ~111 m | Floor / unit level |

The core spec does not mandate a precision level. Regional authorities define precision within their jurisdiction. The local-id carries the remainder.

### 2.4 The Local ID

The local-id segment is assigned by the regional issuing authority within its geographic cell. The core spec defines:

- It is numeric
- It is unique within its geographic cell
- Its length is determined by the issuing authority
- It carries no inherent meaning at the core spec level

How local-ids are assigned, registered, and revoked is defined in the **Governance Addendum** (Section 4) and regional authority policies.

---

## 3. Design Principles

### 3.1 System Agnostic

The GeoAddr core makes no reference to IPv4, IPv6, Ethernet, cellular, satellite, or any specific network technology. It defines an address format and a coordinate system. How that address is carried over a specific network is defined in system-specific addendums.

### 3.2 Addendum Architecture

The spec is intentionally extensible:

```
CORE SPEC
  └── Governance Addendum     (regional authority model)
  └── Resolution Addendum     (how addresses are looked up)
  └── System Addendum: IPv4   (mapping to/from IPv4)
  └── System Addendum: IPv6   (mapping to/from IPv6)
  └── System Addendum: Cellular (mapping to mobile networks)
  └── System Addendum: Satellite (geo-routing for orbital infrastructure)
  └── System Addendum: IoT    (constrained device variant)
  └── [future addendums as new network types emerge]
```

New network technologies do not require changes to the core. They require a new addendum.

### 3.3 Human Layer is Non-Negotiable

Any addendum that introduces a format a human cannot say aloud, write from memory, or recognize on sight is non-conforming. The machine-layer complexity is hidden inside the stack. The human-layer format defined in Section 2.2 is mandatory at all user-facing interfaces.

---

## 4. Governance Model

### 4.1 The Grid Belongs to No One

The coordinate grid is mathematics. No organization owns it. No authority assigns it. It is defined by the physical Earth.

### 4.2 Regional Authorities

Each geographic cell (or contiguous range of cells) maps to a **Regional Issuing Authority (RIA)**. RIAs are recognized entities — governments, intergovernmental bodies, or delegated registries — with jurisdiction over the physical territory within their coordinate range.

RIAs are responsible for:
- Issuing local-ids within their coordinate range
- Maintaining a registry of issued addresses
- Defining sub-regional delegation (cities, ISPs, organizations)
- Enforcing uniqueness within their jurisdiction

### 4.3 No Central Authority Required

The global address space is partitioned by geography. Coordination between RIAs is only required at coordinate boundaries. There is no global registry of individual addresses — only a global registry of which RIA governs which coordinate range.

This registry is small (hundreds of entries, not billions) and changes slowly (only when political boundaries change).

### 4.4 Coordinate Range Assignment

RIA coordinate ranges are derived from existing internationally recognized geographic boundaries (ISO 3166-1 country boundaries as a default baseline). Disputes follow existing international frameworks — not new technical bodies.

---

## 5. Open Questions (v0.1)

These are unresolved at this draft stage and require community input:

1. **Latitude convention** — Should 0 = north pole or 0 = equator? The north-pole-origin (0–180) keeps all values positive and ≤ 360. The equator-origin (-90 to +90) matches scientific convention. Decision pending.

2. **Precision delimiter** — How is decimal precision expressed? Option A: `262.060.4821` (integer only, precision implied by cell registry). Option B: `262.060.48.21` (additional dot groups for sub-cell precision). Option C: `262.0605.4821` (inline decimal in the coordinate).

3. **Non-terrestrial addresses** — Satellites, deep space assets, and orbital infrastructure have no fixed geographic cell. An addendum is needed. Placeholder: a reserved prefix range for non-terrestrial.

4. **Legacy interoperability** — The IPv4 and IPv6 addendums must define bidirectional mapping for the transition period. The mapping strategy (translation layer vs. tunneling vs. dual-address) is not yet defined.

5. **Local-id format** — Should local-ids be purely numeric, or alphanumeric? Numeric is more human-friendly (no case ambiguity, phone-dialable). Decision pending.

6. **Name** — "GeoAddr" is a working title. A permanent name for the protocol is TBD.

---

## 6. Acknowledgments

This specification was developed through human-AI collaborative design. The core orange slice model, the human-first constraint, and the governance-from-geography insight originated in design sessions between the author and Claude (Anthropic). The collaboration itself is a demonstration of the principles the authors also apply to the Candor programming language: explicit, transparent, honest about how the work was done.

---

*Working Draft. Not for implementation. Feedback welcome.*
*Authors: Scott Corley + Claude (Anthropic)*
