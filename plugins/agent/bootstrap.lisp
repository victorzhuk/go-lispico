;; Agent Orchestration Plugin for go-lispico
;; Provides LLM-powered agent definition and coordination
;;
;; Core functions (implemented in Go):
;;   defagent         - define an agent with config options
;;   agent/run        - execute an agent with a prompt
;;   agent/run-parallel - run multiple agents concurrently
;;   agent/run-with-ctx - run with context hashmap
;;   agent/list       - list all registered agents
;;   agent/info        - get agent configuration
;;   agent/route       - route task to appropriate agent
;;   agent/delegate    - delegate task between agents

;; Helper: check if an agent exists
(defn agent/exists? [id]
  (some? (agent/info id)))

;; Helper: run agent with timeout context
;; (agent/run-timeout :my-agent "prompt" 5000)
(defn agent/run-timeout [id prompt timeout-ms]
  (agent/run-with-ctx id prompt {:timeout timeout-ms}))

;; Helper: run multiple prompts on the same agent
;; Returns vector of responses
;; (agent/run-batch :my-agent ["prompt1" "prompt2"])
(defn agent/run-batch [id prompts]
  (map (fn [p] (agent/run id p)) prompts))
