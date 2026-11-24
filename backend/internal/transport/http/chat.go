package http

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

type Chat struct {
	db  *sql.DB
	rdb *redis.Client
}

type Conversation struct {
	ID            int64     `json:"id"`
	OwnerID       int64     `json:"owner_id"`
	OwnerName     string    `json:"owner_name"`
	OrderID       *int64    `json:"order_id,omitempty"`
	ProductID     *int64    `json:"product_id,omitempty"`
	ProductName   string    `json:"product_name,omitempty"`
	LastMessage   string    `json:"last_message,omitempty"`
	LastMessageAt time.Time `json:"last_message_at"`
	CreatedAt     time.Time `json:"created_at"`
}

type ChatMessage struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	SenderID       int64     `json:"sender_id"`
	SenderName     string    `json:"sender_name"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
}

func NewChat(db *sql.DB, redisClient *redis.Client) *Chat {
	return &Chat{db: db, rdb: redisClient}
}

func (chat *Chat) EnsureSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS conversations (
			id BIGSERIAL PRIMARY KEY,
			owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			order_id BIGINT REFERENCES orders(id) ON DELETE SET NULL,
			product_id BIGINT REFERENCES products(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_message_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS conversations_owner_updated_idx ON conversations(owner_user_id, last_message_at DESC, id DESC)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id BIGSERIAL PRIMARY KEY,
			conversation_id BIGINT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			sender_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			client_message_id VARCHAR(80),
			body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 4000),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (conversation_id, sender_id, client_message_id)
		)`,
		`CREATE INDEX IF NOT EXISTS messages_conversation_id_idx ON messages(conversation_id, id DESC)`,
	}
	for _, statement := range statements {
		if _, err := chat.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (chat *Chat) RegisterRoutes(api *gin.RouterGroup, auth *Auth) {
	group := api.Group("/chat", auth.RequireAuth())
	group.GET("/conversations", chat.listConversations)
	group.POST("/conversations", chat.createConversation)
	group.GET("/conversations/:id/messages", chat.listMessages)
	group.POST("/conversations/:id/messages", chat.sendMessage)
	group.POST("/ticket", chat.createWebSocketTicket)
	api.GET("/chat/ws", chat.websocket)
}

func (chat *Chat) listConversations(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	query := `SELECT c.id, c.owner_user_id, owner.name, c.order_id, c.product_id, COALESCE(p.name, ''),
		COALESCE(last.body, ''), c.last_message_at, c.created_at
		FROM conversations c JOIN users owner ON owner.id = c.owner_user_id
		LEFT JOIN products p ON p.id = c.product_id
		LEFT JOIN LATERAL (SELECT body FROM messages WHERE conversation_id = c.id ORDER BY id DESC LIMIT 1) last ON TRUE`
	args := []any{}
	if authRole(c) != "admin" {
		query += ` WHERE c.owner_user_id = $1`
		args = append(args, userID)
	}
	query += ` ORDER BY c.last_message_at DESC, c.id DESC LIMIT 100`
	rows, err := chat.db.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		internalError(c)
		return
	}
	defer rows.Close()
	items := make([]Conversation, 0)
	for rows.Next() {
		var item Conversation
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.OwnerName, &item.OrderID, &item.ProductID, &item.ProductName, &item.LastMessage, &item.LastMessageAt, &item.CreatedAt); err != nil {
			internalError(c)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (chat *Chat) createConversation(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	var input struct {
		OrderID   *int64 `json:"order_id"`
		ProductID *int64 `json:"product_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, "Thông tin hội thoại không hợp lệ")
		return
	}
	if input.OrderID != nil {
		var ownerID int64
		err := chat.db.QueryRowContext(c.Request.Context(), `SELECT user_id FROM orders WHERE id = $1`, *input.OrderID).Scan(&ownerID)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy đơn hàng"})
			return
		}
		if err != nil {
			internalError(c)
			return
		}
		if ownerID != userID && authRole(c) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Bạn không có quyền tạo hội thoại cho đơn hàng này"})
			return
		}
	}
	if input.ProductID != nil {
		var exists bool
		if err := chat.db.QueryRowContext(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, *input.ProductID).Scan(&exists); err != nil {
			internalError(c)
			return
		}
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy sản phẩm"})
			return
		}
	}
	if input.OrderID == nil && input.ProductID == nil && authRole(c) == "admin" {
		badRequest(c, "Admin không thể tạo hội thoại hỗ trợ mới")
		return
	}
	var existingID int64
	err := chat.db.QueryRowContext(c.Request.Context(), `SELECT id FROM conversations WHERE owner_user_id = $1
		AND order_id IS NOT DISTINCT FROM $2 AND product_id IS NOT DISTINCT FROM $3 ORDER BY id DESC LIMIT 1`, userID, input.OrderID, input.ProductID).Scan(&existingID)
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"id": existingID})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		internalError(c)
		return
	}
	var id int64
	if err := chat.db.QueryRowContext(c.Request.Context(), `INSERT INTO conversations (owner_user_id, order_id, product_id) VALUES ($1, $2, $3) RETURNING id`, userID, input.OrderID, input.ProductID).Scan(&id); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (chat *Chat) conversationAccess(ctx context.Context, conversationID, userID int64, role string) error {
	var ownerID int64
	err := chat.db.QueryRowContext(ctx, `SELECT owner_user_id FROM conversations WHERE id = $1`, conversationID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	if ownerID != userID && role != "admin" {
		return errConversationForbidden
	}
	return nil
}

var errConversationForbidden = errors.New("conversation_forbidden")

func parseConversationID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		badRequest(c, "conversation id không hợp lệ")
		return 0, false
	}
	return id, true
}

func (chat *Chat) listMessages(c *gin.Context) {
	id, ok := parseConversationID(c)
	if !ok {
		return
	}
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	if err := chat.conversationAccess(c.Request.Context(), id, userID, authRole(c)); err != nil {
		writeConversationAccessError(c, err)
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = min(parsed, 100)
		}
	}
	before := int64(^uint64(0) >> 1)
	if raw := c.Query("before_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 {
			badRequest(c, "before_id không hợp lệ")
			return
		}
		before = parsed
	}
	rows, err := chat.db.QueryContext(c.Request.Context(), `SELECT m.id, m.conversation_id, m.sender_id, u.name, m.body, m.created_at
		FROM messages m JOIN users u ON u.id = m.sender_id WHERE m.conversation_id = $1 AND m.id < $2 ORDER BY m.id DESC LIMIT $3`, id, before, limit)
	if err != nil {
		internalError(c)
		return
	}
	defer rows.Close()
	items := make([]ChatMessage, 0)
	for rows.Next() {
		var item ChatMessage
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.SenderID, &item.SenderName, &item.Body, &item.CreatedAt); err != nil {
			internalError(c)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		internalError(c)
		return
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (chat *Chat) sendMessage(c *gin.Context) {
	id, ok := parseConversationID(c)
	if !ok {
		return
	}
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	if err := chat.conversationAccess(c.Request.Context(), id, userID, authRole(c)); err != nil {
		writeConversationAccessError(c, err)
		return
	}
	var input struct {
		Body            string `json:"body"`
		ClientMessageID string `json:"client_message_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, "Tin nhắn không hợp lệ")
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" || len([]rune(input.Body)) > 4000 || len(input.ClientMessageID) > 80 {
		badRequest(c, "Tin nhắn phải có từ 1 đến 4000 ký tự")
		return
	}
	var message ChatMessage
	tx, err := chat.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		internalError(c)
		return
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(c.Request.Context(), `INSERT INTO messages (conversation_id, sender_id, client_message_id, body)
		VALUES ($1, $2, NULLIF($3, ''), $4) ON CONFLICT (conversation_id, sender_id, client_message_id) DO UPDATE SET client_message_id = EXCLUDED.client_message_id
		RETURNING id, conversation_id, sender_id, (SELECT name FROM users WHERE id = sender_id), body, created_at`, id, userID, input.ClientMessageID, input.Body).
		Scan(&message.ID, &message.ConversationID, &message.SenderID, &message.SenderName, &message.Body, &message.CreatedAt)
	if err != nil {
		internalError(c)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE conversations SET last_message_at = GREATEST(last_message_at, $1) WHERE id = $2`, message.CreatedAt, id); err != nil {
		internalError(c)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(c)
		return
	}
	encoded, _ := json.Marshal(message)
	_ = chat.rdb.Publish(c.Request.Context(), chatChannel(id), encoded).Err()
	c.JSON(http.StatusCreated, gin.H{"data": message})
}

func (chat *Chat) createWebSocketTicket(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	role := authRole(c)
	var input struct {
		ConversationID int64 `json:"conversation_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.ConversationID < 1 {
		badRequest(c, "conversation_id không hợp lệ")
		return
	}
	if err := chat.conversationAccess(c.Request.Context(), input.ConversationID, userID, role); err != nil {
		writeConversationAccessError(c, err)
		return
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		internalError(c)
		return
	}
	ticket := hex.EncodeToString(random[:])
	payload, _ := json.Marshal(wsTicket{UserID: userID, Role: role, ConversationID: input.ConversationID})
	if err := chat.rdb.Set(c.Request.Context(), "chat:ticket:"+ticket, payload, 45*time.Second).Err(); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ticket": ticket, "expires_in": 45})
}

type wsTicket struct {
	UserID         int64  `json:"user_id"`
	Role           string `json:"role"`
	ConversationID int64  `json:"conversation_id"`
}

func (chat *Chat) websocket(c *gin.Context) {
	ticket := c.Query("ticket")
	if len(ticket) != 64 {
		c.Status(http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	value, err := chat.rdb.GetDel(ctx, "chat:ticket:"+ticket).Bytes()
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	var claims wsTicket
	if json.Unmarshal(value, &claims) != nil || claims.UserID < 1 || claims.ConversationID < 1 {
		c.Status(http.StatusUnauthorized)
		return
	}
	if err := chat.conversationAccess(ctx, claims.ConversationID, claims.UserID, claims.Role); err != nil {
		c.Status(http.StatusForbidden)
		return
	}
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 1024, WriteBufferSize: 1024, CheckOrigin: func(r *http.Request) bool { return allowedWebSocketOrigin(r) }}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	pubsub := chat.rdb.Subscribe(c.Request.Context(), chatChannel(claims.ConversationID))
	defer pubsub.Close()
	if _, err := pubsub.Receive(c.Request.Context()); err != nil {
		return
	}
	messages := pubsub.Channel()
	done := make(chan struct{})
	conn.SetReadLimit(1024)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case msg, open := <-messages:
			if !open {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				return
			}
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func allowedWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	allowedHosts := []string{r.Host, r.Header.Get("X-Forwarded-Host")}
	for _, host := range allowedHosts {
		if host != "" && strings.EqualFold(parsed.Host, host) {
			return true
		}
	}
	// Vite development proxy and local browser access use the same hostname on different ports.
	return strings.EqualFold(parsed.Hostname(), "localhost") || strings.EqualFold(parsed.Hostname(), "127.0.0.1")
}

func chatChannel(id int64) string { return "chat:conversation:" + strconv.FormatInt(id, 10) }

func authRole(c *gin.Context) string {
	role, _ := c.Get("auth_role")
	value, _ := role.(string)
	return value
}

func writeConversationAccessError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy hội thoại"})
	case errors.Is(err, errConversationForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "Bạn không có quyền truy cập hội thoại này"})
	default:
		internalError(c)
	}
}
