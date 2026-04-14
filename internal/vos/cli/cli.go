package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
	"vos/internal/vos/store"
)

type multiString []string

func (values *multiString) String() string {
	return strings.Join(*values, ",")
}

func (values *multiString) Set(raw string) error {
	*values = append(*values, raw)
	return nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	root := flag.NewFlagSet("vos", flag.ContinueOnError)
	root.SetOutput(stderr)
	stateFile := root.String("state-file", ".vos_state.json", "JSON state file path")
	sessionDBFile := root.String("session-db-file", ".vos_sessions.db", "SQLite session database path")
	root.Usage = func() {
		fmt.Fprintln(root.Output(), "Usage:")
		fmt.Fprintln(root.Output(), "  vos [--state-file PATH] [--session-db-file PATH] <topic|node|session> <command> [flags]")
		fmt.Fprintln(root.Output())
		fmt.Fprintln(root.Output(), "Commands:")
		fmt.Fprintln(root.Output(), "  topic   Topic operations")
		fmt.Fprintln(root.Output(), "  node    Node operations")
		fmt.Fprintln(root.Output(), "  session Session operations")
		fmt.Fprintln(root.Output())
		fmt.Fprintln(root.Output(), "Global flags:")
		root.PrintDefaults()
	}

	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	rest := root.Args()
	if len(rest) == 0 {
		root.Usage()
		return 2
	}

	switch rest[0] {
	case "topic":
		svc := service.New(store.NewJSONStateStore(*stateFile))
		return runTopic(svc, rest[1:], stdout, stderr)
	case "node":
		svc := service.New(store.NewJSONStateStore(*stateFile))
		return runNode(svc, rest[1:], stdout, stderr)
	case "session":
		sessionStore, err := store.NewSQLiteSessionStore(*sessionDBFile)
		if err != nil {
			return printError(err, stderr)
		}
		defer sessionStore.Close()
		svc := service.NewWithSessionStore(store.NewJSONStateStore(*stateFile), sessionStore)
		return runSession(svc, rest[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown resource: %s\n", rest[0])
		root.Usage()
		return 2
	}
}

func runTopic(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		printTopicUsage(stderr)
		if len(args) > 0 {
			return 0
		}
		return 2
	}

	switch args[0] {
	case "create":
		return runTopicCreate(svc, args[1:], stdout, stderr)
	case "get":
		return runTopicGet(svc, args[1:], stdout, stderr)
	case "list":
		return runTopicList(svc, args[1:], stdout, stderr)
	case "update":
		return runTopicUpdate(svc, args[1:], stdout, stderr)
	case "delete":
		return runTopicDelete(svc, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown topic command: %s\n", args[0])
		printTopicUsage(stderr)
		return 2
	}
}

func runNode(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		printNodeUsage(stderr)
		if len(args) > 0 {
			return 0
		}
		return 2
	}

	switch args[0] {
	case "create":
		return runNodeCreate(svc, args[1:], stdout, stderr)
	case "get":
		return runNodeGet(svc, args[1:], stdout, stderr)
	case "list":
		return runNodeList(svc, args[1:], stdout, stderr)
	case "children":
		return runNodeChildren(svc, args[1:], stdout, stderr)
	case "move":
		return runNodeMove(svc, args[1:], stdout, stderr)
	case "delete":
		return runNodeDelete(svc, args[1:], stdout, stderr)
	case "update":
		return runNodeUpdate(svc, args[1:], stdout, stderr)
	case "leaf":
		return runNodeLeaf(svc, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown node command: %s\n", args[0])
		printNodeUsage(stderr)
		return 2
	}
}

func runTopicCreate(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos topic create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		topicID      = fs.String("topic-id", "", "Optional topic ID")
		name         = fs.String("name", "", "Topic name")
		description  = fs.String("description", "", "Topic description")
		metadataJSON = fs.String("metadata-json", "{}", "Topic metadata JSON object")
		tagsJSON     = fs.String("tags-json", "[]", "Topic tags JSON string array")
		rootNodeID   = fs.String("root-node-id", "", "Optional root node ID")
		rootNodeName = fs.String("root-node-name", "", "Optional root node name")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos topic create --name NAME [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	metadata, err := parseJSONObject(*metadataJSON, "metadata-json")
	if err != nil {
		return printError(err, stderr)
	}
	tags, err := parseJSONStringList(*tagsJSON, "tags-json")
	if err != nil {
		return printError(err, stderr)
	}

	input := service.CreateTopicInput{
		TopicID:     *topicID,
		Name:        *name,
		Metadata:    metadata,
		Tags:        tags,
		RootNodeID:  *rootNodeID,
		Description: nilIfEmpty(*description),
	}
	if *rootNodeName != "" {
		input.RootNodeName = stringPtr(*rootNodeName)
	}

	topic, rootNode, err := svc.CreateTopic(input)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(map[string]any{"topic": topic, "root_node": rootNode}, stdout, stderr)
}

func runTopicGet(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos topic get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	topicID := fs.String("topic-id", "", "Topic ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos topic get --topic-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	topic, err := svc.GetTopic(*topicID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(topic, stdout, stderr)
}

func runTopicList(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos topic list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos topic list")
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	topics, err := svc.ListTopics()
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(topics, stdout, stderr)
}

func runTopicUpdate(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos topic update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		topicID          = fs.String("topic-id", "", "Topic ID")
		name             = fs.String("name", "", "Updated topic name")
		description      = fs.String("description", "", "Updated topic description")
		clearDescription = fs.Bool("clear-description", false, "Clear topic description")
		metadataJSON     = fs.String("metadata-json", "", "Replace topic metadata with JSON object")
		tagsJSON         = fs.String("tags-json", "", "Replace topic tags with JSON string array")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos topic update --topic-id ID [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	input := service.UpdateTopicInput{
		TopicID:          *topicID,
		Description:      nilIfEmpty(*description),
		ClearDescription: *clearDescription,
	}
	if *name != "" {
		input.Name = stringPtr(*name)
	}
	if *metadataJSON != "" {
		metadata, err := parseJSONObject(*metadataJSON, "metadata-json")
		if err != nil {
			return printError(err, stderr)
		}
		input.Metadata = metadata
		input.ReplaceMetadata = true
	}
	if *tagsJSON != "" {
		tags, err := parseJSONStringList(*tagsJSON, "tags-json")
		if err != nil {
			return printError(err, stderr)
		}
		input.Tags = tags
		input.ReplaceTags = true
	}

	topic, err := svc.UpdateTopic(input)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(topic, stdout, stderr)
}

func runTopicDelete(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos topic delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	topicID := fs.String("topic-id", "", "Topic ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos topic delete --topic-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	result, err := svc.DeleteTopic(*topicID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(result, stdout, stderr)
}

func runNodeCreate(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		topicID     = fs.String("topic-id", "", "Topic ID")
		nodeID      = fs.String("node-id", "", "Optional node ID")
		parentID    = fs.String("parent-id", "", "Parent node ID, default topic root")
		name        = fs.String("name", "", "Node name")
		description = fs.String("description", "", "Node description")
		statusRaw   = fs.String("status", string(domain.NodeStatusDraft), "Initial node status")
		memoryJSON  = fs.String("memory-json", "", "Node memory JSON object")
		inputJSON   = fs.String("input-json", "", "Node input JSON object")
		outputJSON  = fs.String("output-json", "", "Node output JSON object")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node create --topic-id ID --name NAME [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	status, err := domain.ParseNodeStatus(*statusRaw)
	if err != nil {
		return printError(err, stderr)
	}
	memory, err := parseOptionalJSONObject(*memoryJSON, "memory-json")
	if err != nil {
		return printError(err, stderr)
	}
	input, err := parseOptionalJSONObject(*inputJSON, "input-json")
	if err != nil {
		return printError(err, stderr)
	}
	output, err := parseOptionalJSONObject(*outputJSON, "output-json")
	if err != nil {
		return printError(err, stderr)
	}

	inputData := service.CreateNodeInput{
		TopicID:     *topicID,
		NodeID:      *nodeID,
		Name:        *name,
		Description: nilIfEmpty(*description),
		Status:      status,
		Memory:      memory,
		Input:       input,
		Output:      output,
	}
	if *parentID != "" {
		inputData.ParentID = stringPtr(*parentID)
	}

	node, err := svc.CreateNode(inputData)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(node, stdout, stderr)
}

func runNodeGet(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodeID := fs.String("node-id", "", "Node ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node get --node-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	node, err := svc.GetNode(*nodeID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(node, stdout, stderr)
}

func runNodeList(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var includeStatuses multiString
	var excludeStatuses multiString
	var (
		topicID  = fs.String("topic-id", "", "Topic ID")
		leafOnly = fs.Bool("leaf-only", false, "List only leaf nodes")
	)
	fs.Var(&includeStatuses, "status", "Filter to a node status. Repeatable.")
	fs.Var(&excludeStatuses, "exclude-status", "Exclude a node status. Repeatable.")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node list --topic-id ID [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	statuses, err := parseNodeStatuses(includeStatuses, "status")
	if err != nil {
		return printError(err, stderr)
	}
	excluded, err := parseNodeStatuses(excludeStatuses, "exclude-status")
	if err != nil {
		return printError(err, stderr)
	}

	nodes, err := svc.ListNodesByFilter(*topicID, service.NodeListFilter{
		LeafOnly:        *leafOnly,
		Statuses:        statuses,
		ExcludeStatuses: excluded,
	})
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(nodes, stdout, stderr)
}

func runNodeChildren(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node children", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodeID := fs.String("node-id", "", "Node ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node children --node-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	nodes, err := svc.ListChildren(*nodeID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(nodes, stdout, stderr)
}

func runNodeMove(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node move", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodeID := fs.String("node-id", "", "Node ID")
	newParentID := fs.String("new-parent-id", "", "New parent node ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node move --node-id ID --new-parent-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	node, err := svc.MoveNode(*nodeID, *newParentID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(node, stdout, stderr)
}

func runNodeDelete(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodeID := fs.String("node-id", "", "Node ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node delete --node-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	node, err := svc.DeleteNode(*nodeID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(node, stdout, stderr)
}

func runNodeUpdate(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionIDs multiString
	var progress multiString
	var (
		nodeID           = fs.String("node-id", "", "Node ID")
		expectedVersion  = fs.String("expected-version", "", "Require current node version to match before update")
		name             = fs.String("name", "", "Updated node name")
		description      = fs.String("description", "", "Updated node description")
		clearDescription = fs.Bool("clear-description", false, "Clear node description")
		statusRaw        = fs.String("status", "", "Updated node status")
		memoryJSON       = fs.String("memory-json", "", "Node memory JSON object")
		inputJSON        = fs.String("input-json", "", "Node input JSON object")
		outputJSON       = fs.String("output-json", "", "Node output JSON object")
	)
	fs.Var(&sessionIDs, "session-id", "Append one session ID. Repeatable.")
	fs.Var(&progress, "progress", "Append one progress entry. Repeatable.")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node update --node-id ID [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	var versionPtr *int
	if *expectedVersion != "" {
		parsed, err := strconv.Atoi(*expectedVersion)
		if err != nil {
			return printError(domain.ValidationError{Message: "expected-version must be an integer"}, stderr)
		}
		versionPtr = &parsed
	}

	var status *domain.NodeStatus
	if *statusRaw != "" {
		parsed, err := domain.ParseNodeStatus(*statusRaw)
		if err != nil {
			return printError(err, stderr)
		}
		status = &parsed
	}
	memory, err := parseOptionalJSONObject(*memoryJSON, "memory-json")
	if err != nil {
		return printError(err, stderr)
	}
	input, err := parseOptionalJSONObject(*inputJSON, "input-json")
	if err != nil {
		return printError(err, stderr)
	}
	output, err := parseOptionalJSONObject(*outputJSON, "output-json")
	if err != nil {
		return printError(err, stderr)
	}

	update := service.UpdateNodeInput{
		NodeID:           *nodeID,
		ExpectedVersion:  versionPtr,
		Description:      nilIfEmpty(*description),
		ClearDescription: *clearDescription,
		Status:           status,
		Memory:           memory,
		Input:            input,
		Output:           output,
		SessionIDs:       []string(sessionIDs),
		Progress:         []string(progress),
	}
	if *name != "" {
		update.Name = stringPtr(*name)
	}

	node, err := svc.UpdateNode(update)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(node, stdout, stderr)
}

func runNodeLeaf(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node leaf", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodeID := fs.String("node-id", "", "Node ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node leaf --node-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	operable, err := svc.IsLeafOperable(*nodeID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(map[string]any{"node_id": *nodeID, "operable": operable}, stdout, stderr)
}

func printTopicUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  vos topic <create|get|list|update|delete> [flags]")
}

func printNodeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  vos node <create|get|list|children|move|delete|update|leaf> [flags]")
}

func parseFlagSet(fs *flag.FlagSet, args []string) int {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	return -1
}

func dumpJSON(data any, stdout, stderr io.Writer) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		fmt.Fprintf(stderr, "encode output: %v\n", err)
		return 1
	}
	return 0
}

func parseJSONObject(raw, field string) (map[string]any, error) {
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, domain.ValidationError{Message: fmt.Sprintf("invalid JSON for %s: %s", field, err.Error())}
	}
	object, ok := data.(map[string]any)
	if !ok {
		return nil, domain.ValidationError{Message: fmt.Sprintf("%s must be a JSON object", field)}
	}
	return object, nil
}

func parseOptionalJSONObject(raw, field string) (map[string]any, error) {
	if raw == "" {
		return nil, nil
	}
	return parseJSONObject(raw, field)
}

func parseJSONStringList(raw, field string) ([]string, error) {
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, domain.ValidationError{Message: fmt.Sprintf("invalid JSON for %s: %s", field, err.Error())}
	}
	values, ok := data.([]any)
	if !ok {
		return nil, domain.ValidationError{Message: fmt.Sprintf("%s must be a JSON string array", field)}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item, ok := value.(string)
		if !ok {
			return nil, domain.ValidationError{Message: fmt.Sprintf("%s must be a JSON string array", field)}
		}
		result = append(result, item)
	}
	return result, nil
}

func parseNodeStatuses(values []string, field string) ([]domain.NodeStatus, error) {
	if len(values) == 0 {
		return nil, nil
	}
	statuses := make([]domain.NodeStatus, 0, len(values))
	for _, value := range values {
		status, err := domain.ParseNodeStatus(value)
		if err != nil {
			return nil, domain.ValidationError{Message: fmt.Sprintf("invalid %s value %q: %s", field, value, err.Error())}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func printError(err error, stderr io.Writer) int {
	fmt.Fprintln(stderr, err.Error())
	if domain.IsUserFacingError(err) {
		return 2
	}
	return 1
}

func isHelpToken(raw string) bool {
	return raw == "help" || raw == "--help" || raw == "-h"
}

func nilIfEmpty(raw string) *string {
	if raw == "" {
		return nil
	}
	return &raw
}

func stringPtr(raw string) *string {
	return &raw
}
