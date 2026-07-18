; defmacro with quasiquote and unquote-splicing expands against the
; current macro definition, and set! updates the owning global scope.
(defmacro twice [& body] `(do ~@body ~@body))
(def acc 0)
(twice (set! acc (+ acc 1)))
acc
