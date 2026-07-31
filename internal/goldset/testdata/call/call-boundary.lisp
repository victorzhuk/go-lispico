; The Engine.Call boundary with a GoFunc-free callee: the body is one local
; read, so the measured cost is the boundary's own — argument marshalling,
; function-cell lookup, frame setup — not the callee's work. Distinct
; arguments make a wrong-slot read visible in the golden.
(defn call-boundary [a b] a)
