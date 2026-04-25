# GeoAddr — Open Questions
### Working Document v0.1 — 2026-04-17
### Companion to: docs/geoaddr_spec.md

---

## Context for a New Session

GeoAddr is a new internet addressing specification designed from scratch.
The core idea: use Earth's longitude (0–360) and latitude (0–180) as the first two
segments of every address, making geography native to the format.

**Address format:**
```
[longitude].[latitude].[local-id]

262.060.4821    →  Austin, Texas, device 4821
002.041.0093    →  London, England, device 93
139.035.7712    →  Tokyo, Japan, device 7712
```

**Three design principles (from the spec abstract):**
1. **Human-first** — must be easy to say, write, and remember
2. **Geographically meaningful** — the address itself tells you where the endpoint is
3. **System-agnostic** — core spec makes no network assumptions; systems attach via addendums

**Governance:** The coordinate grid belongs to no one (it is mathematics). Regional
Issuing Authorities (RIAs) govern address assignment within their geographic cell range,
derived from existing internationally recognized borders. No central global registry
of individual addresses.

The full spec is at **docs/geoaddr_spec.md**.

---

## Open Questions

These must be resolved before the spec can advance past v0.1.
They are listed in recommended resolution order — earlier questions affect later ones.

---

### Q1 — Latitude Convention

**The question:** Should latitude 0 be the north pole or the equator?

**Option A — North pole origin (0 to 180)**
- 0 = north pole, 90 = equator, 180 = south pole
- All values positive, all values ≤ 180
- Both longitude and latitude stay within 0–360
- Consistent with the "orange slice" mental model (slicing top to bottom)
- No negative numbers anywhere in an address

**Option B — Equator origin (-90 to +90)**
- 0 = equator, +90 = north pole, -90 = south pole
- Matches scientific/cartographic convention (GPS, maps, most software)
- Introduces negative numbers into addresses — complicates the human-first principle
- Familiar to anyone who has read coordinates on a map

**Why it matters:** This decision locks the coordinate system for everything else.
Changing it later breaks every address ever issued.

**Recommended lean:** Option A. Keeping all values positive preserves the human-first
principle — no minus signs, no ambiguity when speaking an address aloud.

**Status:** Undecided.

---

### Q2 — Precision Delimiter

**The question:** How is sub-degree precision expressed in the address format?

**Option A — Integer only, precision implied**
```
262.060.4821
```
Longitude and latitude are always whole degrees. Finer precision is handled
entirely by the regional authority's local-id assignment. Humans never see
decimal coordinates.

**Option B — Additional dot groups**
```
262.060.48.21
```
Sub-cell precision adds more dot-separated groups. Familiar rhythm (like IPv4
expanding). Risk: addresses get longer as precision increases.

**Option C — Inline decimal in coordinate**
```
262.0605.4821
```
Decimal precision embedded in the coordinate segment itself. Compact but
potentially confusing — is `0605` a four-digit number or `06.05`?

**Why it matters:** The delimiter format affects how addresses are spoken, stored,
parsed, and compared. It must be unambiguous and consistent across all precisions.

**Recommended lean:** Option A for the core spec. Sub-degree precision is a
regional registry concern, not a human-facing address concern. Keep the format clean.

**Status:** Undecided.

---

### Q3 — Non-Terrestrial Addresses

**The question:** How are satellites, orbital infrastructure, and deep space assets addressed?

**Background:** The GeoAddr coordinate system is anchored to the Earth's surface.
Satellites move continuously. Deep space assets (probes, stations) are not on Earth at all.

**Options under consideration:**
- Reserved longitude prefix range (e.g., longitude 361–999) for non-terrestrial
- A separate non-terrestrial addendum with its own coordinate system (orbital elements, etc.)
- Orbital assets get a ground station address and are accessed through it

**Why it matters:** Satellite internet (Starlink, Kuiper) is backbone infrastructure now.
This cannot be left as a placeholder in a serious spec.

**Status:** Addendum needed. No draft yet.

---

### Q4 — Legacy Interoperability

**The question:** How do GeoAddr addresses map to and from IPv4 and IPv6 during transition?

**Background:** The existing internet runs on IPv4 and IPv6. GeoAddr cannot replace
them overnight. A transition period of years or decades is realistic. During that period,
GeoAddr endpoints must communicate with IPv4/IPv6 endpoints and vice versa.

**Strategies under consideration:**
- **Translation layer** — a gateway translates GeoAddr ↔ IPv4/IPv6 at the boundary
- **Tunneling** — GeoAddr packets are encapsulated inside IPv4/IPv6 (like 6PE does for IPv6 over IPv4 MPLS)
- **Dual addressing** — every endpoint carries both a GeoAddr address and an IPv4/IPv6 address during transition

**Why it matters:** The adoption path depends on this. If translation is too lossy or
tunneling adds too much overhead, GeoAddr stays a research project.

**Status:** Undecided. System addendums for IPv4 and IPv6 are not yet drafted.

---

### Q5 — Local-ID Format

**The question:** Should local-ids be purely numeric or alphanumeric?

**Option A — Numeric only**
```
262.060.4821
```
- Phone-dialable
- No case ambiguity (uppercase B vs. 8, etc.)
- Consistent with the human-first principle
- Limits address space per cell to 10^N where N is digit count

**Option B — Alphanumeric**
```
262.060.X4B1
```
- Larger address space per cell (more devices per geographic cell)
- Introduces ambiguity when spoken aloud or handwritten
- Consistent with how MAC addresses and some serial numbers work

**Why it matters:** Numeric-only is simpler for humans but may be insufficient
for dense urban cells with millions of devices. This may be resolvable by
allowing regional authorities to choose, within bounds set by the core spec.

**Recommended lean:** Numeric only in the core spec. Alphanumeric as an
optional extension in the regional governance addendum for high-density cells.

**Status:** Undecided.

---

### Q6 — Protocol Name

**The question:** "GeoAddr" is a working title. What is the permanent name?

**Criteria for a good name:**
- Easy to say and spell
- Not already claimed by another protocol or product
- Conveys the geographic + addressing idea
- Works as an adjective ("a GeoAddr address") and a noun ("the GeoAddr spec")

**Working candidates:**
- GeoAddr
- GeoNet
- OrangeNet (references the orange slice mental model)
- GeoID
- LatLon (too technical)
- GeoSlice

**Status:** Undecided. Low priority until the technical questions are resolved,
but the name affects everything from community building to spec branding.

---

## Resolution Log

| # | Question | Status | Decision | Date |
|---|---|---|---|---|
| Q1 | Latitude convention | Open | — | — |
| Q2 | Precision delimiter | Open | — | — |
| Q3 | Non-terrestrial addresses | Open | — | — |
| Q4 | Legacy interoperability | Open | — | — |
| Q5 | Local-id format | Open | — | — |
| Q6 | Protocol name | Open | — | — |

---

*Update this log as decisions are made. Each resolved question should be backported
into the main spec (docs/geoaddr_spec.md) before closing.*
