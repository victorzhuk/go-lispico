---
status: accepted
---

# A fully covering embedder may own evaluation deadlines

The Engine retains its safe 30-second default, but it does not create a redundant timer when the caller already has an earlier deadline. An embedder may select `WithTimeout(0)` only after it applies a deadline to every evaluation lifecycle; YAGEL does so once the separate Rule-load and handler-dispatch deadlines from YAGEL ADR 0042 cover all paths. Keeping both timers was rejected because the hidden Engine limit cannot express YAGEL's distinct lifecycles, while removing the Engine default globally was rejected because ordinary embedders still need a safe default.