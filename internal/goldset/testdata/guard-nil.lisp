; False when and negated-true when produce nil in value positions, composing
; inside let, do, and vectors without stack underflow.
(def skipped (when false :a))
(def blocked (when (not true) :b))
[skipped blocked]
