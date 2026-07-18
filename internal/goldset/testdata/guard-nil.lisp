; False when and true unless produce nil in value positions, composing
; inside let, do, and vectors without stack underflow.
(def skipped (when false :a))
(def blocked (unless true :b))
[skipped blocked]
