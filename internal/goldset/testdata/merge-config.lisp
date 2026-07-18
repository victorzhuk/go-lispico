; merge keeps immutable inputs and right-most precedence.
(def base {:mode :tree :depth 10 :trace false})
(def override {:depth 50 :trace true})
(def cfg (merge base override))
[(get cfg :mode) (get cfg :depth) (get cfg :trace)]
