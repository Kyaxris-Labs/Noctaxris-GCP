package pubsub

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// HTTPPrincipal extracts the authenticated principal from an HTTP request.
type HTTPPrincipal func(r *http.Request) (authn.Principal, bool)

// RegisterREST mounts Pub/Sub v1 REST routes (Terraform / REST clients).
func (s *Service) RegisterREST(mux *http.ServeMux, principal HTTPPrincipal) {
	h := &restHandler{svc: s, principal: principal}
	mux.HandleFunc("GET /v1/projects/{project}/topics", h.listTopics)
	mux.HandleFunc("PUT /v1/projects/{project}/topics/{topic}", h.createOrReplaceTopic)
	mux.HandleFunc("GET /v1/projects/{project}/topics/{topic}", h.getTopic)
	mux.HandleFunc("PATCH /v1/projects/{project}/topics/{topic}", h.patchTopic)
	mux.HandleFunc("DELETE /v1/projects/{project}/topics/{topic}", h.deleteTopic)
	mux.HandleFunc("POST /v1/projects/{project}/topics/{topic}", h.topicPost)

	mux.HandleFunc("GET /v1/projects/{project}/subscriptions", h.listSubscriptions)
	mux.HandleFunc("PUT /v1/projects/{project}/subscriptions/{subscription}", h.createOrReplaceSubscription)
	mux.HandleFunc("GET /v1/projects/{project}/subscriptions/{subscription}", h.getSubscription)
	mux.HandleFunc("PATCH /v1/projects/{project}/subscriptions/{subscription}", h.patchSubscription)
	mux.HandleFunc("DELETE /v1/projects/{project}/subscriptions/{subscription}", h.deleteSubscription)
	mux.HandleFunc("POST /v1/projects/{project}/subscriptions/{subscription}", h.subscriptionPost)
}

type restHandler struct {
	svc       *Service
	principal HTTPPrincipal
}

func (h *restHandler) require(w http.ResponseWriter, r *http.Request, permission, resource string) bool {
	if h.principal == nil {
		gcperrors.Unauthenticated(w, "")
		return false
	}
	p, ok := h.principal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return false
	}
	allowed, err := h.svc.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return false
	}
	if !allowed {
		gcperrors.PermissionDenied(w, "")
		return false
	}
	return true
}

func splitColon(v string) (id, action string) {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func topicName(project, topicID string) string {
	return "projects/" + project + "/topics/" + topicID
}

func subName(project, subID string) string {
	return "projects/" + project + "/subscriptions/" + subID
}

func (h *restHandler) listTopics(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if !h.require(w, r, "pubsub.topics.list", projectResource(project)) {
		return
	}
	list, err := h.svc.Store.ListTopics(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	topics := make([]map[string]any, 0, len(list))
	for i := range list {
		topics = append(topics, topicJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": topics})
}

func (h *restHandler) createOrReplaceTopic(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	topicID, _ := splitColon(r.PathValue("topic"))
	if !h.require(w, r, "pubsub.topics.create", projectResource(project)) {
		return
	}
	var body struct {
		Labels map[string]string `json:"labels"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	name := topicName(project, topicID)
	t, created, err := h.svc.Store.CreateTopicWithLabels(name, project, body.Labels)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "topic already exists")
		return
	}
	writeJSON(w, http.StatusOK, topicJSON(t))
}

func (h *restHandler) getTopic(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	topicID, _ := splitColon(r.PathValue("topic"))
	if !h.require(w, r, "pubsub.topics.get", projectResource(project)) {
		return
	}
	t, ok, err := h.svc.Store.GetTopic(topicName(project, topicID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "topic not found")
		return
	}
	writeJSON(w, http.StatusOK, topicJSON(t))
}

func (h *restHandler) patchTopic(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	topicID, _ := splitColon(r.PathValue("topic"))
	if !h.require(w, r, "pubsub.topics.update", projectResource(project)) {
		return
	}
	var body struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid topic patch body")
		return
	}
	t, err := h.svc.Store.UpdateTopicLabels(topicName(project, topicID), body.Labels)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "topic not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, topicJSON(t))
}

func (h *restHandler) deleteTopic(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	topicID, _ := splitColon(r.PathValue("topic"))
	if !h.require(w, r, "pubsub.topics.delete", projectResource(project)) {
		return
	}
	ok, err := h.svc.Store.DeleteTopic(topicName(project, topicID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "topic not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (h *restHandler) topicPost(w http.ResponseWriter, r *http.Request) {
	_, action := splitColon(r.PathValue("topic"))
	switch action {
	case "publish":
		h.publish(w, r)
	default:
		gcperrors.InvalidArgument(w, "unsupported topics custom method")
	}
}

func (h *restHandler) publish(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	topicID, _ := splitColon(r.PathValue("topic"))
	if !h.require(w, r, "pubsub.topics.publish", projectResource(project)) {
		return
	}
	name := topicName(project, topicID)
	if _, ok, err := h.svc.Store.GetTopic(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "topic not found")
		return
	}
	var body struct {
		Messages []struct {
			Data       string            `json:"data"`
			Attributes map[string]string `json:"attributes"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid publish body")
		return
	}
	ids := make([]string, 0, len(body.Messages))
	for _, m := range body.Messages {
		data, err := base64.StdEncoding.DecodeString(m.Data)
		if err != nil {
			// REST also accepts raw UTF-8 in some clients; try plain.
			data = []byte(m.Data)
		}
		id, copies, err := h.svc.Store.PublishFanout(name, data, m.Attributes)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		h.svc.deliverPush(copies)
		ids = append(ids, id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messageIds": ids})
}

func (h *restHandler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if !h.require(w, r, "pubsub.subscriptions.list", projectResource(project)) {
		return
	}
	list, err := h.svc.Store.ListSubscriptions(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	subs := make([]map[string]any, 0, len(list))
	for i := range list {
		subs = append(subs, subscriptionJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs})
}

func (h *restHandler) createOrReplaceSubscription(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.create", projectResource(project)) {
		return
	}
	var body struct {
		Topic              string            `json:"topic"`
		AckDeadlineSeconds int               `json:"ackDeadlineSeconds"`
		PushConfig         *struct {
			PushEndpoint string `json:"pushEndpoint"`
		} `json:"pushConfig"`
		Labels map[string]string `json:"labels"`
		Filter string            `json:"filter"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid subscription body")
		return
	}
	push := ""
	if body.PushConfig != nil {
		push = body.PushConfig.PushEndpoint
	}
	created, ok, err := h.svc.Store.CreateSubscriptionFull(
		subName(project, subID), body.Topic, project, body.AckDeadlineSeconds, push, body.Labels, body.Filter,
	)
	if err != nil {
		if strings.Contains(err.Error(), "topic not found") {
			gcperrors.NotFound(w, "topic not found")
			return
		}
		if strings.Contains(err.Error(), "invalid filter") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "subscription already exists")
		return
	}
	writeJSON(w, http.StatusOK, subscriptionJSON(created))
}

func (h *restHandler) getSubscription(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.get", projectResource(project)) {
		return
	}
	sub, ok, err := h.svc.Store.GetSubscription(subName(project, subID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "subscription not found")
		return
	}
	writeJSON(w, http.StatusOK, subscriptionJSON(sub))
}

func (h *restHandler) patchSubscription(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.update", projectResource(project)) {
		return
	}
	var body struct {
		AckDeadlineSeconds *int `json:"ackDeadlineSeconds"`
		PushConfig         *struct {
			PushEndpoint string `json:"pushEndpoint"`
		} `json:"pushConfig"`
		Labels *map[string]string `json:"labels"`
		Filter *string            `json:"filter"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid subscription patch body")
		return
	}
	var push *string
	if body.PushConfig != nil {
		ep := body.PushConfig.PushEndpoint
		push = &ep
	}
	updated, err := h.svc.Store.UpdateSubscription(subName(project, subID), body.AckDeadlineSeconds, push, body.Labels, body.Filter)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "subscription not found")
			return
		}
		if strings.Contains(err.Error(), "invalid filter") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, subscriptionJSON(updated))
}

func (h *restHandler) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.delete", projectResource(project)) {
		return
	}
	ok, err := h.svc.Store.DeleteSubscription(subName(project, subID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "subscription not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (h *restHandler) subscriptionPost(w http.ResponseWriter, r *http.Request) {
	_, action := splitColon(r.PathValue("subscription"))
	switch action {
	case "pull":
		h.pull(w, r)
	case "acknowledge":
		h.acknowledge(w, r)
	case "modifyAckDeadline":
		h.modifyAckDeadline(w, r)
	case "modifyPushConfig":
		h.modifyPushConfig(w, r)
	case "seek":
		h.seek(w, r)
	default:
		gcperrors.InvalidArgument(w, "unsupported subscriptions custom method")
	}
}

func (h *restHandler) pull(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.consume", projectResource(project)) {
		return
	}
	var body struct {
		MaxMessages int `json:"maxMessages"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	msgs, err := h.svc.Store.Pull(subName(project, subID), body.MaxMessages)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "subscription not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	received := make([]map[string]any, 0, len(msgs))
	for i := range msgs {
		attrs := map[string]string{}
		if msgs[i].AttributesJSON != "" && msgs[i].AttributesJSON != "{}" {
			_ = json.Unmarshal([]byte(msgs[i].AttributesJSON), &attrs)
		}
		received = append(received, map[string]any{
			"ackId": msgs[i].AckID,
			"message": map[string]any{
				"data":        base64.StdEncoding.EncodeToString(msgs[i].Data),
				"messageId":   msgs[i].ID,
				"attributes":  attrs,
				"publishTime": msgs[i].PublishTime,
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"receivedMessages": received})
}

func (h *restHandler) acknowledge(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.consume", projectResource(project)) {
		return
	}
	var body struct {
		AckIds []string `json:"ackIds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid acknowledge body")
		return
	}
	if err := h.svc.Store.Acknowledge(subName(project, subID), body.AckIds); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (h *restHandler) modifyAckDeadline(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.consume", projectResource(project)) {
		return
	}
	var body struct {
		AckIds             []string `json:"ackIds"`
		AckDeadlineSeconds int      `json:"ackDeadlineSeconds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid modifyAckDeadline body")
		return
	}
	if err := h.svc.Store.ModifyAckDeadline(subName(project, subID), body.AckIds, body.AckDeadlineSeconds); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (h *restHandler) modifyPushConfig(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.update", projectResource(project)) {
		return
	}
	var body struct {
		PushConfig *struct {
			PushEndpoint string `json:"pushEndpoint"`
		} `json:"pushConfig"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid modifyPushConfig body")
		return
	}
	ep := ""
	if body.PushConfig != nil {
		ep = body.PushConfig.PushEndpoint
	}
	updated, err := h.svc.Store.UpdateSubscription(subName(project, subID), nil, &ep, nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "subscription not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, subscriptionJSON(updated))
}

func (h *restHandler) seek(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	subID, _ := splitColon(r.PathValue("subscription"))
	if !h.require(w, r, "pubsub.subscriptions.consume", projectResource(project)) {
		return
	}
	var body struct {
		Time string `json:"time"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid seek body")
		return
	}
	if body.Time == "" {
		gcperrors.InvalidArgument(w, "seek time required (snapshots not supported)")
		return
	}
	t, err := time.Parse(time.RFC3339Nano, body.Time)
	if err != nil {
		t, err = time.Parse(time.RFC3339, body.Time)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid seek time")
			return
		}
	}
	if err := h.svc.Store.SeekToTime(subName(project, subID), t); err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "subscription not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func topicJSON(t *store.PubSubTopic) map[string]any {
	labels := t.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return map[string]any{"name": t.Name, "labels": labels}
}

func subscriptionJSON(sub *store.PubSubSubscription) map[string]any {
	labels := sub.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	out := map[string]any{
		"name":               sub.Name,
		"topic":              sub.Topic,
		"ackDeadlineSeconds": sub.AckDeadlineSeconds,
		"labels":             labels,
	}
	if sub.Filter != "" {
		out["filter"] = sub.Filter
	}
	if sub.PushEndpoint != "" {
		out["pushConfig"] = map[string]any{"pushEndpoint": sub.PushEndpoint}
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
