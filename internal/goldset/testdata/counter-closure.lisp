; set! mutates the existing lexical owner, so closure state persists
; across invocations and a fresh scope is not silently created.
(def make-counter
  (fn []
    (let [n 0]
      (fn [] (do (set! n (+ n 1)) n)))))
(def c (make-counter))
(c)
(c)
(c)
