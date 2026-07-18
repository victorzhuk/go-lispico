; Keyword values stay callable map lookups: hit, missing key, and a
; non-map argument all behave identically under both execution modes.
(def m {:a 1 :b 2})
[(:a m) (:missing m) (:a 42)]
