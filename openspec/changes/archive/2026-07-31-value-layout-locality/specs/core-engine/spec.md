# core-engine — delta

## ADDED Requirements

### Requirement: Boxed value memory layout is size-class-aware

Concrete `Value` struct layouts MAY be tuned for Go allocator size-class
efficiency — field width and ordering chosen so a boxed value's memory
footprint lands in a smaller size class — provided the change is justified
by measurement on a workload that actually exercises the affected
representation at scale, not applied speculatively. Any such layout change
SHALL leave observable semantics, equality, printing, and public field
ranges (or their documented reduction, e.g. a narrowed maximum length)
unchanged for realistic inputs, and SHALL NOT alter any value's accounted
allocation-ledger size: the ledger's fixed size table (ADR 0011) is
independent of a Go struct's actual memory layout, and a layout tuning change
SHALL NOT be allowed to move it.

#### Scenario: A layout change is measurement-justified

- **WHEN** a `Value` type's struct layout is changed for size-class efficiency
- **THEN** the change SHALL be accompanied by a benchmark demonstrating a measurable improvement on a workload exercising that representation at a realistic scale, not merely a theoretical size-class crossing

#### Scenario: Accounted ledger size is unaffected by layout tuning

- **WHEN** a `Value` type's Go struct layout changes size for allocator efficiency
- **THEN** that type's accounted allocation-ledger size (as computed from ADR 0011's fixed size table) SHALL remain exactly what it was before the layout change

#### Scenario: A narrowed field range is documented, not silent

- **WHEN** a layout change narrows a field's representable range (for example, capping a count field's width)
- **THEN** the resulting limit SHALL be documented, and behavior at or beyond that limit SHALL fail closed rather than silently wrap or corrupt
