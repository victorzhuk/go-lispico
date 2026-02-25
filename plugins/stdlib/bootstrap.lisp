;; Threading Macros for go-lispico stdlib
;; These are loaded automatically by the stdlib plugin

;; -> (thread-first): inserts x as second element of each form
;; Example: (-> 1 (+ 2) (* 3)) => (* (+ 1 2) 3) => 9
(defmacro -> [x & forms]
  (loop [acc x
         fs forms]
    (if (empty? fs)
      acc
      (let* [form (first fs)
             threaded (if (list? form)
                        (cons (first form) (cons acc (rest form)))
                        (list form acc))]
        (recur threaded (rest fs))))))

;; ->> (thread-last): appends x as last element of each form
;; Example: (->> [1 2 3] (map inc) (filter pos?)) => (filter pos? (map inc [1 2 3]))
(defmacro ->> [x & forms]
  (loop [acc x
         fs forms]
    (if (empty? fs)
      acc
      (let* [form (first fs)
             threaded (if (list? form)
                        (concat form (list acc))
                        (list form acc))]
        (recur threaded (rest fs))))))

;; as->: thread with named binding so position is explicit
;; Example: (as-> 5 x (* x 2) (+ x 3)) => (+ (* 5 2) 3) => 13
(defmacro as-> [expr name & forms]
  (let* [pairs (loop [acc []
                      fs forms]
                 (if (empty? fs)
                   acc
                   (recur (conj acc name (first fs)) (rest fs))))]
    (list (quote let) (apply vector (conj pairs name expr)) name)))

;; if-let: bind and branch on truthiness
;; Example: (if-let [x (get map :key)] x :not-found)
(defmacro if-let [bindings then else]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (list (quote if) name then else))))

;; when-let: bind and execute body if truthy
;; Example: (when-let [x (get map :key)] (process x) x)
(defmacro when-let [bindings & body]
  (let* [name (first bindings)
         val (first (rest bindings))]
    (list (quote let) (vector name val)
      (cons (quote when) (cons name body)))))

;; get-in: nested key access via reduce over get
;; Example: (get-in {:a {:b 1}} [:a :b]) => 1
(defn get-in [m ks]
  (reduce (fn [acc k] (get acc k)) m ks))
