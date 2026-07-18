; Higher-order pipeline: filter odds, square, fold -- the shape of a
; handler transforming an event batch.
(reduce + 0
  (map (fn [x] (* x x))
       (filter (fn [x] (= (mod x 2) 1)) [1 2 3 4 5 6 7 8 9 10])))
