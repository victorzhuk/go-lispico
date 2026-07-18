; Example fixture pending the YAGEL-exported gold set: folding a tool
; vector into a registry map through reduce/assoc, asserted via a
; print-stable scalar rather than map ordering.
(defn register [registry tool]
  (assoc registry (:name tool) tool))
(def tools [{:name "search" :arity 2}
            {:name "fetch" :arity 1}
            {:name "exec" :arity 3}])
(count (reduce register {} tools))
