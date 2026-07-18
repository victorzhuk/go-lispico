; loop/recur iterates in constant stack; sum of 0..99.
(loop [i 0 acc 0]
  (if (= i 100) acc (recur (+ i 1) (+ acc i))))
