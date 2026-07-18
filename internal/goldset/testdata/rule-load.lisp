; Startup-shaped cell: loading a rule file is dominated by reading and
; defining many handlers, not by applying one.
(defn on-boot [e] :boot-ok)
(defn on-shutdown [e] :shutdown-ok)
(defn on-message [e] (:msg e))
(defn on-tool-call [e] [:dispatch (:tool e)])
(defn on-tool-result [e] [:reply (:result e)])
(defn on-error [e] [:isolate (:reason e)])
(defn on-timeout [e] :retry)
(defn on-cancel [e] :cancelled)
(defn on-register [e] [:registered (:name e)])
(defn on-unregister [e] [:removed (:name e)])
(defn on-heartbeat [e] :alive)
(defn on-audit [e] :audit-ok)
[(on-boot {:id 1}) (on-audit {:id 12})]
