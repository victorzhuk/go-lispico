; Data-dominated string building from an event map.
(defn render [event]
  (str "[" (:level event) "] " (:msg event)))
(render {:level :warn :msg "cache stale"})
