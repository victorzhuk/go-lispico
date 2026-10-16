(def make-rule
  (fn [offset]
    (fn [x] (+ x offset))))
(def rule-a (make-rule 1))
(def rule-b (make-rule 2))
(list (rule-a 100) (rule-b 100))
