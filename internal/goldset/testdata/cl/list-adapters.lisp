(let ((values (sort (list 3 1 2) #'<)))
  (list (nth 1 values)
        (mapcar (fn (x) (* x 2)) values)
        (length values)))
