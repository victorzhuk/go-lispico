; Example fixture pending the YAGEL-exported gold set: a rule-shaped
; handler dispatching on a keyword-called event map through flat cond.
(defn route [event]
  (cond
    (= (:type event) :tool-call) :dispatch
    (= (:type event) :message) :reply
    :else :ignore))
(route {:type :message :from "user"})
