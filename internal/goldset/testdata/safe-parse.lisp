; A catch binding exists only in the handler scope: locals bound after a
; try body keep their slots on both the normal and the error path.
; let* binds sequentially, so label sees r.
(defn attempt [x]
  (let* [r (try (if (= x :bad) (throw "boom") x) (catch e :caught))
         label (if (= r :caught) :failed :ok)]
    [label r]))
[(attempt :good) (attempt :bad)]
