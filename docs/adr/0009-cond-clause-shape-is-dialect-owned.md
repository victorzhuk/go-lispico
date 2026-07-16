---
status: accepted
---

# `cond` clause shape is normalized by the Dialect

The Clojure Dialect accepts flat test/expression pairs while the Common Lisp Dialect retains nested clauses. One Dialect-owned clause normalizer produces canonical clauses for both Evaluator and Compiler at special-form dispatch; it does not rewrite Reader output, so quoted data remains untouched. A canonical clause is one test plus one body expression: a Common Lisp clause with a multi-expression implicit-progn body normalizes by wrapping the body in kernel `do`. Separate execution-path parsers were rejected because they can drift, and macro desugaring was rejected because a kernel form must not depend on stdlib bootstrap.