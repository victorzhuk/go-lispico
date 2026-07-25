; An audit queue crosses vectorFlatThreshold (32) mid-accumulation and
; promotes from a flat buffer to a trie; the fold below sums it after.
(let [queue (loop [i 0 q []]
              (if (= i 40)
                q
                (recur (+ i 1) (conj q i))))]
  (reduce + 0 queue))
