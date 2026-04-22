package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"vos/internal/openmate/observability"
	omruntime "vos/internal/openmate/runtime"
	"vos/internal/vos/domain"
	"vos/internal/vos/service"
)

const (
	v1Prefix            = "/api/v1"
	notImplementedV1Msg = "not implemented in vos adapter yet"
)

type Config struct {
	StateFile        string
	SessionDBFile    string
	WorkspaceRoot    string
	ModelConfig      string
	ScheduleDB       string
	ScheduleMode     string
	ScheduleCmd      []string
	WorkerCommand    []string
	DefaultTimeoutMS int
	AgingSeconds     int
	Logger           *slog.Logger
}

type Server struct {
	service              *service.Service
	runtime              *omruntime.Runtime
	decomposeRunner      service.NodeDecomposeRunner
	mux                  *http.ServeMux
	handler              http.Handler
	stateFile            string
	sessionDB            string
	workspace            string
	modelConfig          string
	scheduleDB           string
	scheduleMode         string
	scheduleCmd          []string
	scheduleTickInterval time.Duration
	scheduleMu           sync.Mutex
	chatRunsMu           sync.Mutex
	chatRuns             map[string]*chatRun
	logger               *slog.Logger
}

type v1Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type nodeIncludeSet map[string]bool

func NewServer(config Config) (*Server, error) {
	logger := observability.NormalizeLogger(config.Logger).With(
		slog.String(observability.FieldComponent, "vos-api"),
	)
	if strings.TrimSpace(config.StateFile) == "" {
		logger.Error("state_file must not be empty")
		return nil, domain.ValidationError{Message: "state_file must not be empty"}
	}
	if strings.TrimSpace(config.SessionDBFile) == "" {
		logger.Error("session_db_file must not be empty")
		return nil, domain.ValidationError{Message: "session_db_file must not be empty"}
	}

	workspaceRoot := strings.TrimSpace(config.WorkspaceRoot)
	if workspaceRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root: %w", err)
		}
		workspaceRoot = cwd
	}
	workspaceRoot = filepath.Clean(workspaceRoot)

	modelConfig := strings.TrimSpace(config.ModelConfig)
	if modelConfig == "" {
		modelConfig = filepath.Join(workspaceRoot, "model.json")
	}

	scheduleMode := strings.ToLower(strings.TrimSpace(config.ScheduleMode))
	if scheduleMode == "" {
		scheduleMode = "inproc"
	}
	if scheduleMode != "inproc" && scheduleMode != "shell" {
		logger.Error("schedule_mode must be one of: inproc, shell", slog.String("schedule_mode", scheduleMode))
		return nil, domain.ValidationError{Message: "schedule_mode must be one of: inproc, shell"}
	}

	scheduleDB := strings.TrimSpace(config.ScheduleDB)
	if scheduleDB == "" {
		scheduleDB = strings.TrimSpace(config.SessionDBFile)
	}
	if scheduleDB == "" {
		logger.Error("schedule_db must not be empty")
		return nil, domain.ValidationError{Message: "schedule_db must not be empty"}
	}

	defaultTimeoutMS := config.DefaultTimeoutMS
	if defaultTimeoutMS <= 0 {
		defaultTimeoutMS = 120000
	}
	agingThreshold := time.Duration(config.AgingSeconds) * time.Second
	if agingThreshold <= 0 {
		agingThreshold = 10 * time.Minute
	}
	tickInterval := chatPollInterval

	runtime, err := omruntime.Open(omruntime.Config{
		StateFile:       config.StateFile,
		SessionDBFile:   config.SessionDBFile,
		ScheduleDBFile:  scheduleDB,
		WorkspaceRoot:   workspaceRoot,
		ModelConfigFile: modelConfig,
		WorkerCommand:   config.WorkerCommand,
		DefaultTimeout:  defaultTimeoutMS,
		AgingThreshold:  agingThreshold,
		Logger:          logger,
	})
	if err != nil {
		logger.Error("open runtime failed", slog.Any("error", err))
		return nil, err
	}

	scheduleCmd := make([]string, 0, len(config.ScheduleCmd))
	for _, value := range config.ScheduleCmd {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		scheduleCmd = append(scheduleCmd, trimmed)
	}
	if scheduleMode == "shell" && len(scheduleCmd) == 0 {
		scheduleCmd = defaultScheduleCommand(workspaceRoot)
	}
	if scheduleMode == "shell" && len(scheduleCmd) == 0 {
		_ = runtime.Close()
		logger.Error("schedule_cmd must not be empty in shell mode")
		return nil, domain.ValidationError{Message: "schedule_cmd must not be empty"}
	}

	server := &Server{
		service:              runtime.Service,
		runtime:              runtime,
		decomposeRunner:      service.NewCommandDecomposeRunner(service.DefaultDecomposeAgentCommand()),
		mux:                  http.NewServeMux(),
		stateFile:            filepath.Clean(config.StateFile),
		sessionDB:            filepath.Clean(config.SessionDBFile),
		workspace:            workspaceRoot,
		modelConfig:          filepath.Clean(modelConfig),
		scheduleDB:           filepath.Clean(scheduleDB),
		scheduleMode:         scheduleMode,
		scheduleCmd:          scheduleCmd,
		scheduleTickInterval: tickInterval,
		chatRuns:             map[string]*chatRun{},
		logger:               logger,
	}
	server.registerRoutes()
	server.handler = server.wrapAPIHeaders(server.mux)
	logger.Info(
		"vos api server initialized",
		slog.String("schedule_mode", scheduleMode),
		slog.String("schedule_db", filepath.Clean(scheduleDB)),
	)
	return server, nil
}

func (server *Server) Handler() http.Handler {
	return server.handler
}

func (server *Server) Close() error {
	if server.runtime == nil {
		return nil
	}
	err := server.runtime.Close()
	if err != nil {
		observability.NormalizeLogger(server.logger).Error("close runtime failed", slog.Any("error", err))
		return err
	}
	observability.NormalizeLogger(server.logger).Info("server closed")
	return nil
}

func (server *Server) registerRoutes() {
	server.mux.HandleFunc(v1Prefix+"/health", server.handleV1Health)

	server.mux.HandleFunc(v1Prefix+"/topics", server.handleV1Topics)
	server.mux.HandleFunc(v1Prefix+"/topics/", server.handleV1TopicRoutes)

	server.mux.HandleFunc(v1Prefix+"/nodes", server.handleV1Nodes)
	server.mux.HandleFunc(v1Prefix+"/nodes/", server.handleV1NodeRoutes)

	server.mux.HandleFunc(v1Prefix+"/tree", server.handleV1TreeEntry)
	server.mux.HandleFunc(v1Prefix+"/tree/", server.handleV1TreeEntry)

	server.mux.HandleFunc(v1Prefix+"/chat", server.handleV1ChatEntry)
	server.mux.HandleFunc(v1Prefix+"/chat/", server.handleV1ChatEntry)
	server.mux.HandleFunc(v1Prefix+"/topic", server.handleV1Unimplemented)
	server.mux.HandleFunc(v1Prefix+"/topic/", server.handleV1Unimplemented)
	server.mux.HandleFunc(v1Prefix+"/planlist", server.handleV1Unimplemented)
	server.mux.HandleFunc(v1Prefix+"/planlist/", server.handleV1Unimplemented)
	server.mux.HandleFunc(v1Prefix+"/stats", server.handleV1Unimplemented)
	server.mux.HandleFunc(v1Prefix+"/stats/", server.handleV1Unimplemented)
}

func (server *Server) handleV1Health(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet)
		return
	}
	server.writeV1Success(writer, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (server *Server) handleV1Topics(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		topics, err := server.service.ListTopics()
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		server.writeV1Success(writer, topics)
	case http.MethodPost:
		var payload createTopicPayload
		if err := decodeJSON(request.Body, &payload); err != nil {
			server.writeV1Error(writer, http.StatusBadRequest, err.Error())
			return
		}

		input := service.CreateTopicInput{
			TopicID:     strings.TrimSpace(payload.TopicID),
			Name:        strings.TrimSpace(payload.Name),
			Description: normalizeOptionalString(payload.Description),
			Metadata:    payload.Metadata,
			Tags:        payload.Tags,
			RootNodeID:  strings.TrimSpace(payload.RootNodeID),
		}
		if value := normalizeOptionalString(payload.RootNodeName); value != nil {
			input.RootNodeName = value
		}

		topic, rootNode, err := server.service.CreateTopic(input)
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		nodeView, err := server.buildV1NodeView(rootNode, nodeIncludeSet{})
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		server.writeV1Success(writer, map[string]any{
			"topic":     topic,
			"root_node": nodeView,
		})
	default:
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet, http.MethodPost)
	}
}

func (server *Server) handleV1TopicRoutes(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, v1Prefix+"/topics/")
	if path == "" {
		server.writeV1Error(writer, http.StatusNotFound, "not found")
		return
	}

	if strings.HasSuffix(path, "/nodes") {
		topicID := strings.TrimSuffix(path, "/nodes")
		topicID = strings.TrimSuffix(topicID, "/")
		if topicID == "" || strings.Contains(topicID, "/") {
			server.writeV1Error(writer, http.StatusNotFound, "not found")
			return
		}
		server.handleV1TopicNodes(writer, request, topicID)
		return
	}

	topicID := strings.TrimSuffix(path, "/")
	if topicID == "" || strings.Contains(topicID, "/") {
		server.writeV1Error(writer, http.StatusNotFound, "not found")
		return
	}

	switch request.Method {
	case http.MethodGet:
		topic, err := server.service.GetTopic(topicID)
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		server.writeV1Success(writer, topic)
	case http.MethodDelete:
		result, err := server.service.DeleteTopic(topicID)
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		server.writeV1Success(writer, result)
	case http.MethodPatch:
		var payload updateTopicPayload
		if err := decodeJSON(request.Body, &payload); err != nil {
			server.writeV1Error(writer, http.StatusBadRequest, err.Error())
			return
		}
		input := service.UpdateTopicInput{
			TopicID:          topicID,
			Description:      normalizeOptionalString(payload.Description),
			ClearDescription: payload.ClearDescription,
		}
		if value := normalizeOptionalString(payload.Name); value != nil {
			input.Name = value
		}
		if payload.Metadata != nil {
			input.Metadata = payload.Metadata
			input.ReplaceMetadata = true
		}
		if payload.Tags != nil {
			input.Tags = payload.Tags
			input.ReplaceTags = true
		}
		topic, err := server.service.UpdateTopic(input)
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		server.writeV1Success(writer, topic)
	default:
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet, http.MethodDelete, http.MethodPatch)
	}
}

func (server *Server) handleV1TopicNodes(writer http.ResponseWriter, request *http.Request, topicID string) {
	if request.Method != http.MethodGet {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet)
		return
	}

	leafOnly, err := parseOptionalBool(request.URL.Query().Get("leaf_only"))
	if err != nil {
		server.writeV1Error(writer, http.StatusBadRequest, err.Error())
		return
	}

	statuses, err := parseV1StatusFilters(request.URL.Query()["status"])
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	excludeStatuses, err := parseV1StatusFilters(request.URL.Query()["exclude_status"])
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}

	nodes, err := server.service.ListNodesByFilter(topicID, service.NodeListFilter{
		LeafOnly:        leafOnly,
		Statuses:        statuses,
		ExcludeStatuses: excludeStatuses,
	})
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}

	payload := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		view, err := server.buildV1NodeView(node, nodeIncludeSet{})
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		payload = append(payload, view)
	}
	server.writeV1Success(writer, payload)
}

func (server *Server) handleV1Nodes(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodPost)
		return
	}

	var payload v1CreateNodePayload
	if err := decodeJSON(request.Body, &payload); err != nil {
		server.writeV1Error(writer, http.StatusBadRequest, err.Error())
		return
	}

	topicID := normalizeOptionalString(payload.TopicID)
	parentID := normalizeOptionalString(payload.ParentID)
	trimmedName := strings.TrimSpace(payload.Name)

	if trimmedName == "" {
		trimmedName = "Untitled Node"
	}

	input := service.CreateNodeInput{
		NodeID:      strings.TrimSpace(payload.NodeID),
		Name:        trimmedName,
		Description: normalizeOptionalString(payload.Description),
		Memory:      payload.Memory,
		Input:       payload.Input,
		Output:      payload.Output,
	}
	if topicID != nil {
		input.TopicID = *topicID
	}
	if parentID != nil {
		input.ParentID = parentID
	}
	if payload.Status != nil && strings.TrimSpace(*payload.Status) != "" {
		status, err := parseV1NodeStatus(*payload.Status)
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		input.Status = status
	}

	node, err := server.service.CreateNode(input)
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}

	nodeView, err := server.buildV1NodeView(node, nodeIncludeSet{})
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	nodeView["topic_id"] = node.TopicID
	server.writeV1Success(writer, nodeView)
}

func (server *Server) handleV1NodeRoutes(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, v1Prefix+"/nodes/")
	if path == "" {
		server.writeV1Error(writer, http.StatusNotFound, "not found")
		return
	}

	if strings.HasSuffix(path, "/decompose") {
		nodeID := strings.TrimSuffix(path, "/decompose")
		nodeID = strings.TrimSuffix(nodeID, "/")
		if nodeID == "" || strings.Contains(nodeID, "/") {
			server.writeV1Error(writer, http.StatusNotFound, "not found")
			return
		}
		server.handleV1NodeDecompose(writer, request, nodeID)
		return
	}

	if strings.HasSuffix(path, "/children") {
		nodeID := strings.TrimSuffix(path, "/children")
		nodeID = strings.TrimSuffix(nodeID, "/")
		if nodeID == "" || strings.Contains(nodeID, "/") {
			server.writeV1Error(writer, http.StatusNotFound, "not found")
			return
		}
		server.handleV1NodeChildren(writer, request, nodeID)
		return
	}
	if strings.HasSuffix(path, "/move") {
		nodeID := strings.TrimSuffix(path, "/move")
		nodeID = strings.TrimSuffix(nodeID, "/")
		if nodeID == "" || strings.Contains(nodeID, "/") {
			server.writeV1Error(writer, http.StatusNotFound, "not found")
			return
		}
		server.handleV1NodeMove(writer, request, nodeID)
		return
	}
	if strings.HasSuffix(path, "/leaf") {
		nodeID := strings.TrimSuffix(path, "/leaf")
		nodeID = strings.TrimSuffix(nodeID, "/")
		if nodeID == "" || strings.Contains(nodeID, "/") {
			server.writeV1Error(writer, http.StatusNotFound, "not found")
			return
		}
		server.handleV1NodeLeaf(writer, request, nodeID)
		return
	}

	nodeID := strings.TrimSuffix(path, "/")
	if nodeID == "" || strings.Contains(nodeID, "/") {
		server.writeV1Error(writer, http.StatusNotFound, "not found")
		return
	}

	switch request.Method {
	case http.MethodGet:
		node, err := server.service.GetNode(nodeID)
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		include := parseV1IncludeSet(request.URL.Query().Get("include"))
		nodeView, err := server.buildV1NodeView(node, include)
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		nodeView["topic_id"] = node.TopicID
		server.writeV1Success(writer, nodeView)
	case http.MethodPatch:
		var payload v1UpdateNodePayload
		if err := decodeJSON(request.Body, &payload); err != nil {
			server.writeV1Error(writer, http.StatusBadRequest, err.Error())
			return
		}
		input := service.UpdateNodeInput{
			NodeID:           nodeID,
			ExpectedVersion:  payload.ExpectedVersion,
			Description:      normalizeOptionalString(payload.Description),
			ClearDescription: payload.ClearDescription,
			Memory:           payload.Memory,
			Input:            payload.Input,
			Output:           payload.Output,
			SessionIDs:       payload.SessionIDs,
			Process:          payload.Process,
		}
		if value := normalizeOptionalString(payload.Name); value != nil {
			input.Name = value
		}
		if payload.Status != nil {
			status, err := parseV1NodeStatus(strings.TrimSpace(*payload.Status))
			if err != nil {
				server.writeV1ServiceError(writer, err)
				return
			}
			input.Status = &status
		}
		if _, err := server.service.UpdateNode(input); err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		server.writeV1Success(writer, nil)
	case http.MethodDelete:
		if _, err := server.service.DeleteNode(nodeID); err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		server.writeV1Success(writer, nil)
	default:
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet, http.MethodPatch, http.MethodDelete)
	}
}

func (server *Server) handleV1NodeChildren(writer http.ResponseWriter, request *http.Request, nodeID string) {
	if request.Method != http.MethodGet {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet)
		return
	}
	nodes, err := server.service.ListChildren(nodeID)
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	payload := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		view, err := server.buildV1NodeView(node, nodeIncludeSet{})
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		view["topic_id"] = node.TopicID
		payload = append(payload, view)
	}
	server.writeV1Success(writer, payload)
}

func (server *Server) handleV1NodeMove(writer http.ResponseWriter, request *http.Request, nodeID string) {
	if request.Method != http.MethodPost {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodPost)
		return
	}
	var payload moveNodePayload
	if err := decodeJSON(request.Body, &payload); err != nil {
		server.writeV1Error(writer, http.StatusBadRequest, err.Error())
		return
	}
	newParentID := strings.TrimSpace(payload.NewParentID)
	node, err := server.service.MoveNode(nodeID, newParentID)
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	view, err := server.buildV1NodeView(node, nodeIncludeSet{})
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	view["topic_id"] = node.TopicID
	server.writeV1Success(writer, view)
}

func (server *Server) handleV1NodeLeaf(writer http.ResponseWriter, request *http.Request, nodeID string) {
	if request.Method != http.MethodGet {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet)
		return
	}
	operable, err := server.service.IsLeafOperable(nodeID)
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	server.writeV1Success(writer, map[string]any{
		"node_id":  nodeID,
		"operable": operable,
	})
}

func (server *Server) handleV1NodeDecompose(writer http.ResponseWriter, request *http.Request, nodeID string) {
	if request.Method != http.MethodPost {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodPost)
		return
	}

	var payload v1NodeDecomposePayload
	if err := decodeJSON(request.Body, &payload); err != nil {
		server.writeV1Error(writer, http.StatusBadRequest, err.Error())
		return
	}

	hint := ""
	if payload.Hint != nil {
		hint = strings.TrimSpace(*payload.Hint)
	}
	maxItems := service.DefaultNodeDecomposeMaxItems
	if payload.MaxItems != nil {
		maxItems = *payload.MaxItems
	}

	result, err := server.service.DecomposeNode(request.Context(), service.NodeDecomposeInput{
		NodeID:   nodeID,
		Hint:     hint,
		MaxItems: maxItems,
	}, server.decomposeRunner)
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	server.writeV1Success(writer, result)
}

func (server *Server) handleV1TreeEntry(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, v1Prefix+"/tree")
	switch path {
	case "", "/":
		server.handleV1Tree(writer, request)
	case "/roots":
		server.handleV1TreeRoots(writer, request)
	case "/generate":
		server.handleV1Unimplemented(writer, request)
	default:
		server.writeV1Error(writer, http.StatusNotFound, "not found")
	}
}

func (server *Server) handleV1Tree(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet)
		return
	}

	rootID := strings.TrimSpace(request.URL.Query().Get("root_id"))
	if rootID == "" {
		roots, err := server.collectRootNodes()
		if err != nil {
			server.writeV1ServiceError(writer, err)
			return
		}
		if len(roots) == 0 {
			server.writeV1Success(writer, map[string]any{})
			return
		}
		rootID = roots[0].ID
	}

	treeNode, err := server.buildV1TreeNode(rootID)
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	server.writeV1Success(writer, treeNode)
}

func (server *Server) handleV1TreeRoots(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet)
		return
	}

	roots, err := server.collectRootNodes()
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}

	payload := make([]v1RootNodeSummary, 0, len(roots))
	for _, node := range roots {
		payload = append(payload, v1RootNodeSummary{
			ID:            node.ID,
			Name:          node.Name,
			Status:        fromDomainNodeStatus(node.Status),
			CreatedAt:     node.CreatedAt,
			UpdatedAt:     node.UpdatedAt,
			ChildrenCount: len(node.ChildrenIDs),
		})
	}
	server.writeV1Success(writer, payload)
}

func (server *Server) handleV1Unimplemented(writer http.ResponseWriter, request *http.Request) {
	server.writeV1Error(writer, http.StatusNotImplemented, notImplementedV1Msg)
}

func (server *Server) collectRootNodes() ([]*domain.Node, error) {
	return server.service.ListDisplayRootNodes()
}

func (server *Server) buildV1TreeNode(nodeID string) (*v1TreeNodeResponse, error) {
	node, err := server.service.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	children := make([]*v1TreeNodeResponse, 0, len(node.ChildrenIDs))
	for _, childID := range node.ChildrenIDs {
		child, err := server.buildV1TreeNode(childID)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return &v1TreeNodeResponse{
		ID:        node.ID,
		Name:      node.Name,
		Status:    fromDomainNodeStatus(node.Status),
		UpdatedAt: node.UpdatedAt,
		Children:  children,
	}, nil
}

func (server *Server) buildV1NodeView(node *domain.Node, include nodeIncludeSet) (map[string]any, error) {
	view := map[string]any{
		"id":           node.ID,
		"name":         node.Name,
		"parent_id":    node.ParentID,
		"children_ids": cloneStringSlice(node.ChildrenIDs),
		"status":       fromDomainNodeStatus(node.Status),
		"created_at":   node.CreatedAt,
		"updated_at":   node.UpdatedAt,
	}

	if include["memory"] {
		view["memory"] = cloneMapOrEmpty(node.Memory)
	}
	if include["input"] {
		view["input"] = cloneMapOrEmpty(node.Input)
	}
	if include["output"] {
		view["output"] = cloneMapOrEmpty(node.Output)
	}
	if include["process"] {
		view["process"] = cloneProcessItems(node.Process)
	}
	if include["session"] {
		messages, err := server.buildV1SessionMessages(node.Session)
		if err != nil {
			return nil, err
		}
		view["session"] = messages
	}

	return view, nil
}

func (server *Server) buildV1SessionMessages(sessionIDs []string) ([]v1SessionMessage, error) {
	messages := make([]v1SessionMessage, 0)
	for _, sessionID := range sessionIDs {
		events, err := server.service.ListSessionEvents(sessionID, 0, 1000)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			role := ""
			if event.Role != nil {
				role = string(*event.Role)
			}
			if role == "" {
				role = readOptionalString(event.PayloadJSON, "role")
			}
			if role == "" {
				continue
			}
			content := extractV1MessageContent(event.PayloadJSON)
			if content == "" {
				continue
			}
			messages = append(messages, v1SessionMessage{
				Role:      role,
				Content:   content,
				Timestamp: event.CreatedAt.Format(time.RFC3339Nano),
			})
		}
	}
	return messages, nil
}

func readOptionalString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	raw, exists := payload[key]
	if !exists {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func extractV1MessageContent(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if value := readOptionalString(payload, "content"); value != "" {
		return value
	}
	if value := readOptionalString(payload, "text"); value != "" {
		return value
	}
	if value := readOptionalString(payload, "message"); value != "" {
		return value
	}

	rawContent, exists := payload["content"]
	if exists {
		if text := extractTextFromAny(rawContent); text != "" {
			return text
		}
	}
	rawOutput, exists := payload["output"]
	if exists {
		if text := extractTextFromAny(rawOutput); text != "" {
			return text
		}
	}
	return ""
}

func extractTextFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		collected := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := extractTextFromAny(item); text != "" {
				collected = append(collected, text)
			}
		}
		return strings.Join(collected, "\n")
	case map[string]any:
		if text, ok := typed["text"].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if text, ok := typed["content"].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if inner, ok := typed["content"]; ok {
			if text := extractTextFromAny(inner); text != "" {
				return text
			}
		}
		if inner, ok := typed["output"]; ok {
			if text := extractTextFromAny(inner); text != "" {
				return text
			}
		}
	}
	return ""
}

func parseV1IncludeSet(raw string) nodeIncludeSet {
	values := nodeIncludeSet{}
	for _, token := range strings.Split(raw, ",") {
		key := strings.ToLower(strings.TrimSpace(token))
		if key == "" {
			continue
		}
		values[key] = true
	}
	return values
}

func parseV1StatusFilters(values []string) ([]domain.NodeStatus, error) {
	if len(values) == 0 {
		return nil, nil
	}
	parsed := make([]domain.NodeStatus, 0)
	for _, value := range values {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			raw := strings.TrimSpace(part)
			if raw == "" {
				continue
			}
			status, err := parseV1NodeStatus(raw)
			if err != nil {
				return nil, err
			}
			parsed = append(parsed, status)
		}
	}
	return parsed, nil
}

func parseV1NodeStatus(raw string) (domain.NodeStatus, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "pending", "draft":
		return domain.NodeStatusDraft, nil
	case "waiting", "ready":
		return domain.NodeStatusReady, nil
	case "running", "active":
		return domain.NodeStatusActive, nil
	case "failed", "blocked":
		return domain.NodeStatusBlocked, nil
	case "completed", "done":
		return domain.NodeStatusDone, nil
	default:
		return domain.ParseNodeStatus(value)
	}
}

func fromDomainNodeStatus(status domain.NodeStatus) string {
	switch status {
	case domain.NodeStatusDraft:
		return "pending"
	case domain.NodeStatusReady:
		return "waiting"
	case domain.NodeStatusActive:
		return "running"
	case domain.NodeStatusBlocked:
		return "failed"
	case domain.NodeStatusDone:
		return "completed"
	default:
		return string(status)
	}
}

func cloneStringSlice(raw []string) []string {
	if raw == nil {
		return []string{}
	}
	cloned := make([]string, len(raw))
	copy(cloned, raw)
	return cloned
}

func cloneProcessItems(raw []domain.ProcessItem) []domain.ProcessItem {
	if raw == nil {
		return []domain.ProcessItem{}
	}
	cloned := make([]domain.ProcessItem, len(raw))
	copy(cloned, raw)
	return cloned
}

func cloneMapOrEmpty(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(raw))
	for key, value := range raw {
		cloned[key] = value
	}
	return cloned
}

func (server *Server) wrapAPIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, v1Prefix+"/") {
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
			if request.Method == http.MethodOptions {
				writer.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) writeV1Success(writer http.ResponseWriter, data any) {
	server.writeV1Envelope(writer, http.StatusOK, http.StatusOK, "ok", data)
}

func (server *Server) writeV1Error(writer http.ResponseWriter, status int, message string) {
	server.writeV1Envelope(writer, status, status, message, nil)
}

func (server *Server) writeV1Envelope(writer http.ResponseWriter, httpStatus, code int, message string, data any) {
	server.writeJSON(writer, httpStatus, v1Envelope{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

func (server *Server) writeV1ServiceError(writer http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.As(err, &domain.ValidationError{}):
		status = http.StatusBadRequest
	case errors.As(err, &domain.TopicNotFoundError{}), errors.As(err, &domain.NodeNotFoundError{}), errors.As(err, &domain.SessionNotFoundError{}):
		status = http.StatusNotFound
	case errors.As(err, &domain.DuplicateEntityError{}), errors.As(err, &domain.VersionConflictError{}), errors.As(err, &domain.SessionSequenceConflictError{}):
		status = http.StatusConflict
	case domain.IsUserFacingError(err):
		status = http.StatusBadRequest
	}
	server.writeV1Error(writer, status, err.Error())
}

func (server *Server) writeV1MethodNotAllowed(writer http.ResponseWriter, method string, allowed ...string) {
	writer.Header().Set("Allow", strings.Join(allowed, ", "))
	server.writeV1Error(writer, http.StatusMethodNotAllowed, fmt.Sprintf("method not allowed: %s", method))
}

func (server *Server) writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func parseOptionalBool(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, domain.ValidationError{Message: "leaf_only must be a boolean"}
	}
	return value, nil
}

type createTopicPayload struct {
	TopicID      string         `json:"topic_id"`
	Name         string         `json:"name"`
	Description  *string        `json:"description"`
	Metadata     map[string]any `json:"metadata"`
	Tags         []string       `json:"tags"`
	RootNodeID   string         `json:"root_node_id"`
	RootNodeName *string        `json:"root_node_name"`
}

type updateTopicPayload struct {
	Name             *string        `json:"name"`
	Description      *string        `json:"description"`
	ClearDescription bool           `json:"clear_description"`
	Metadata         map[string]any `json:"metadata"`
	Tags             []string       `json:"tags"`
}

type v1CreateNodePayload struct {
	TopicID     *string        `json:"topic_id"`
	NodeID      string         `json:"node_id"`
	ParentID    *string        `json:"parent_id"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	Status      *string        `json:"status"`
	Memory      map[string]any `json:"memory"`
	Input       map[string]any `json:"input"`
	Output      map[string]any `json:"output"`
	Metadata    map[string]any `json:"metadata"`
	Tags        []string       `json:"tags"`
}

type v1UpdateNodePayload struct {
	ExpectedVersion  *int                 `json:"expected_version"`
	Name             *string              `json:"name"`
	Description      *string              `json:"description"`
	ClearDescription bool                 `json:"clear_description"`
	Status           *string              `json:"status"`
	Memory           map[string]any       `json:"memory"`
	Input            map[string]any       `json:"input"`
	Output           map[string]any       `json:"output"`
	SessionIDs       []string             `json:"session_ids"`
	Process          []domain.ProcessItem `json:"process"`
}

type v1NodeDecomposePayload struct {
	Hint     *string `json:"hint"`
	MaxItems *int    `json:"max_items"`
}

type moveNodePayload struct {
	NewParentID string `json:"new_parent_id"`
}

type v1TreeNodeResponse struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Status    string                `json:"status"`
	UpdatedAt time.Time             `json:"updated_at"`
	Children  []*v1TreeNodeResponse `json:"children"`
}

type v1RootNodeSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ChildrenCount int       `json:"children_count"`
}

type v1SessionMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp,omitempty"`
}
