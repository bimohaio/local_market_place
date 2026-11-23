package http

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Orders struct{ db *sql.DB }

type CartLine struct {
	ProductID int64   `json:"product_id"`
	Name      string  `json:"name"`
	Slug      string  `json:"slug"`
	ImageURL  string  `json:"image_url"`
	Price     float64 `json:"price"`
	Stock     int     `json:"stock"`
	Quantity  int     `json:"quantity"`
	LineTotal float64 `json:"line_total"`
}

type OrderLine struct {
	ID          int64   `json:"id"`
	ProductID   int64   `json:"product_id"`
	ProductName string  `json:"product_name"`
	ProductSlug string  `json:"product_slug"`
	ImageURL    string  `json:"image_url"`
	UnitPrice   float64 `json:"unit_price"`
	Quantity    int     `json:"quantity"`
	LineTotal   float64 `json:"line_total"`
}

type OrderStatusChange struct {
	FromStatus string    `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status"`
	ChangedBy  *int64    `json:"changed_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Order struct {
	ID            int64               `json:"id"`
	Code          string              `json:"code"`
	UserID        int64               `json:"user_id"`
	Email         string              `json:"email"`
	RecipientName string              `json:"recipient_name"`
	Phone         string              `json:"phone"`
	Address       string              `json:"address"`
	Note          string              `json:"note"`
	PaymentMethod string              `json:"payment_method"`
	PaymentStatus string              `json:"payment_status"`
	Status        string              `json:"status"`
	Subtotal      float64             `json:"subtotal"`
	ShippingFee   float64             `json:"shipping_fee"`
	Total         float64             `json:"total"`
	Items         []OrderLine         `json:"items,omitempty"`
	StatusHistory []OrderStatusChange `json:"status_history,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

func NewOrders(db *sql.DB) *Orders { return &Orders{db: db} }

func (orders *Orders) EnsureSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS carts (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS cart_items (
			cart_id BIGINT NOT NULL REFERENCES carts(id) ON DELETE CASCADE,
			product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
			quantity INTEGER NOT NULL CHECK (quantity > 0),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (cart_id, product_id)
		)`,
		`CREATE TABLE IF NOT EXISTS orders (
			id BIGSERIAL PRIMARY KEY,
			code VARCHAR(40) NOT NULL UNIQUE,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			email TEXT NOT NULL,
			recipient_name VARCHAR(120) NOT NULL,
			phone VARCHAR(40) NOT NULL,
			address TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			payment_method VARCHAR(30) NOT NULL CHECK (payment_method IN ('cod')),
			payment_status VARCHAR(30) NOT NULL CHECK (payment_status IN ('unpaid', 'paid', 'failed', 'refunded', 'cancelled')),
			status VARCHAR(30) NOT NULL CHECK (status IN ('confirmed', 'processing', 'shipped', 'delivered', 'cancelled')),
			subtotal NUMERIC(12,2) NOT NULL CHECK (subtotal >= 0),
			shipping_fee NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK (shipping_fee >= 0),
			total NUMERIC(12,2) NOT NULL CHECK (total >= 0),
			idempotency_key VARCHAR(100) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (user_id, idempotency_key)
		)`,
		`CREATE INDEX IF NOT EXISTS orders_user_created_idx ON orders(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS orders_status_created_idx ON orders(status, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS order_items (
			id BIGSERIAL PRIMARY KEY,
			order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
			product_name VARCHAR(180) NOT NULL,
			product_slug VARCHAR(200) NOT NULL,
			image_url TEXT NOT NULL DEFAULT '',
			unit_price NUMERIC(12,2) NOT NULL CHECK (unit_price >= 0),
			quantity INTEGER NOT NULL CHECK (quantity > 0),
			line_total NUMERIC(12,2) NOT NULL CHECK (line_total >= 0)
		)`,
		`CREATE INDEX IF NOT EXISTS order_items_order_id_idx ON order_items(order_id)`,
		`CREATE TABLE IF NOT EXISTS payments (
			id BIGSERIAL PRIMARY KEY,
			order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
			provider VARCHAR(40) NOT NULL,
			provider_reference VARCHAR(200) NOT NULL DEFAULT '',
			amount NUMERIC(12,2) NOT NULL CHECK (amount >= 0),
			status VARCHAR(30) NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed', 'refunded', 'cancelled')),
			paid_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS payments_order_id_idx ON payments(order_id)`,
		`CREATE TABLE IF NOT EXISTS inventory_movements (
			id BIGSERIAL PRIMARY KEY,
			product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
			order_id BIGINT REFERENCES orders(id) ON DELETE RESTRICT,
			user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
			quantity_delta INTEGER NOT NULL CHECK (quantity_delta <> 0),
			reason VARCHAR(40) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS inventory_movements_product_created_idx ON inventory_movements(product_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS order_status_history (
			id BIGSERIAL PRIMARY KEY,
			order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			from_status VARCHAR(30),
			to_status VARCHAR(30) NOT NULL,
			changed_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS order_status_history_order_created_idx ON order_status_history(order_id, created_at)`,
	}
	for _, statement := range statements {
		if _, err := orders.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (orders *Orders) RegisterRoutes(api *gin.RouterGroup, auth *Auth) {
	user := api.Group("", auth.RequireAuth())
	user.GET("/cart", orders.getCart)
	user.POST("/cart/items", orders.addCartItem)
	user.PUT("/cart/items/:product_id", orders.setCartItem)
	user.DELETE("/cart/items/:product_id", orders.deleteCartItem)
	user.POST("/checkout", orders.checkout)
	user.GET("/orders", orders.listOwnOrders)
	user.GET("/orders/:id", orders.getOwnOrder)
	user.POST("/orders/:id/cancel", orders.cancelOwnOrder)

	admin := api.Group("/admin", auth.RequireAuth(), auth.RequireRole("admin"))
	admin.GET("/orders", orders.listAdminOrders)
	admin.GET("/orders/:id", orders.getAdminOrder)
	admin.PATCH("/orders/:id/status", orders.updateOrderStatus)
}

func authUserID(c *gin.Context) (int64, bool) {
	value, ok := c.Get("auth_user_id")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return 0, false
	}
	userID, ok := value.(int64)
	if !ok || userID < 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return 0, false
	}
	return userID, true
}

func (orders *Orders) getCart(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	rows, err := orders.db.QueryContext(c.Request.Context(), `
		SELECT p.id, p.name, p.slug, p.image_url, p.price, p.stock, ci.quantity, p.price * ci.quantity
		FROM carts ca JOIN cart_items ci ON ci.cart_id = ca.id JOIN products p ON p.id = ci.product_id
		WHERE ca.user_id = $1 ORDER BY ci.created_at ASC`, userID)
	if err != nil {
		internalError(c)
		return
	}
	defer rows.Close()
	items := make([]CartLine, 0)
	var total float64
	var itemCount int
	for rows.Next() {
		var item CartLine
		if err := rows.Scan(&item.ProductID, &item.Name, &item.Slug, &item.ImageURL, &item.Price, &item.Stock, &item.Quantity, &item.LineTotal); err != nil {
			internalError(c)
			return
		}
		items = append(items, item)
		total += item.LineTotal
		itemCount += item.Quantity
	}
	if err := rows.Err(); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "item_count": itemCount})
}

type cartItemInput struct {
	ProductID int64 `json:"product_id"`
	Quantity  int   `json:"quantity"`
}

func (orders *Orders) addCartItem(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	var input cartItemInput
	if err := c.ShouldBindJSON(&input); err != nil || input.ProductID < 1 || input.Quantity < 1 || input.Quantity > 99 {
		badRequest(c, "product_id và quantity không hợp lệ (quantity từ 1 đến 99)")
		return
	}
	if err := orders.changeCartItem(c, userID, input.ProductID, input.Quantity, true); err != nil {
		writeCartError(c, err)
		return
	}
	orders.getCart(c)
}

func (orders *Orders) setCartItem(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	productID, err := strconv.ParseInt(c.Param("product_id"), 10, 64)
	if err != nil || productID < 1 {
		badRequest(c, "product_id không hợp lệ")
		return
	}
	var input struct {
		Quantity int `json:"quantity"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Quantity < 1 || input.Quantity > 99 {
		badRequest(c, "quantity phải từ 1 đến 99")
		return
	}
	if err := orders.changeCartItem(c, userID, productID, input.Quantity, false); err != nil {
		writeCartError(c, err)
		return
	}
	orders.getCart(c)
}

func (orders *Orders) changeCartItem(c *gin.Context, userID, productID int64, quantity int, add bool) error {
	tx, err := orders.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cartID int64
	err = tx.QueryRowContext(c.Request.Context(), `INSERT INTO carts (user_id) VALUES ($1) ON CONFLICT (user_id) DO UPDATE SET updated_at = NOW() RETURNING id`, userID).Scan(&cartID)
	if err != nil {
		return err
	}
	var stock int
	var active bool
	err = tx.QueryRowContext(c.Request.Context(), `SELECT stock, is_active FROM products WHERE id = $1 FOR UPDATE`, productID).Scan(&stock, &active)
	if err != nil {
		return err
	}
	if !active {
		return errProductUnavailable
	}
	var current int
	err = tx.QueryRowContext(c.Request.Context(), `SELECT quantity FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cartID, productID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
	} else if err != nil {
		return err
	}
	next := quantity
	if add {
		next += current
	}
	if next > stock {
		return errInsufficientStock
	}
	if _, err = tx.ExecContext(c.Request.Context(), `INSERT INTO cart_items (cart_id, product_id, quantity) VALUES ($1, $2, $3)
		ON CONFLICT (cart_id, product_id) DO UPDATE SET quantity = EXCLUDED.quantity, updated_at = NOW()`, cartID, productID, next); err != nil {
		return err
	}
	return tx.Commit()
}

func (orders *Orders) deleteCartItem(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	productID, err := strconv.ParseInt(c.Param("product_id"), 10, 64)
	if err != nil || productID < 1 {
		badRequest(c, "product_id không hợp lệ")
		return
	}
	tx, err := orders.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		internalError(c)
		return
	}
	defer tx.Rollback()
	var cartID int64
	err = tx.QueryRowContext(c.Request.Context(), `SELECT id FROM carts WHERE user_id = $1 FOR UPDATE`, userID).Scan(&cartID)
	if errors.Is(err, sql.ErrNoRows) {
		orders.getCart(c)
		return
	}
	if err != nil {
		internalError(c)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cartID, productID); err != nil {
		internalError(c)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(c)
		return
	}
	orders.getCart(c)
}

var (
	errProductUnavailable  = errors.New("product_unavailable")
	errInsufficientStock   = errors.New("insufficient_stock")
	errOrderNotFound       = errors.New("order_not_found")
	errOrderNotCancellable = errors.New("order_not_cancellable")
)

func writeCartError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, errProductUnavailable):
		c.JSON(http.StatusNotFound, gin.H{"error": "Sản phẩm không còn khả dụng"})
	case errors.Is(err, errInsufficientStock):
		c.JSON(http.StatusConflict, gin.H{"error": "Số lượng vượt quá tồn kho hiện tại"})
	default:
		internalError(c)
	}
}

type checkoutInput struct {
	RecipientName string `json:"recipient_name"`
	Phone         string `json:"phone"`
	Address       string `json:"address"`
	Note          string `json:"note"`
	PaymentMethod string `json:"payment_method"`
}

type checkoutItem struct {
	productID int64
	name      string
	slug      string
	image     string
	price     float64
	stock     int
	quantity  int
}

func (orders *Orders) checkout(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	var input checkoutInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, "Thông tin checkout không hợp lệ")
		return
	}
	input.RecipientName = strings.TrimSpace(input.RecipientName)
	input.Phone = strings.TrimSpace(input.Phone)
	input.Address = strings.TrimSpace(input.Address)
	input.Note = strings.TrimSpace(input.Note)
	if len(input.RecipientName) < 2 || len(input.RecipientName) > 120 || input.Phone == "" || len(input.Phone) > 40 || input.Address == "" || len(input.Address) > 1000 || len(input.Note) > 1000 {
		badRequest(c, "Tên người nhận, số điện thoại và địa chỉ là bắt buộc")
		return
	}
	if input.PaymentMethod == "" {
		input.PaymentMethod = "cod"
	}
	if input.PaymentMethod != "cod" {
		badRequest(c, "Hiện chỉ hỗ trợ thanh toán khi nhận hàng (COD)")
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" || len(key) > 100 {
		badRequest(c, "Idempotency-Key bắt buộc và tối đa 100 ký tự")
		return
	}
	userEmail, _ := c.Get("auth_email")
	email, _ := userEmail.(string)
	tx, err := orders.db.BeginTx(c.Request.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		internalError(c)
		return
	}
	defer tx.Rollback()
	var cartID int64
	err = tx.QueryRowContext(c.Request.Context(), `SELECT id FROM carts WHERE user_id = $1 FOR UPDATE`, userID).Scan(&cartID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusConflict, gin.H{"error": "Giỏ hàng đang trống"})
		return
	}
	if err != nil {
		internalError(c)
		return
	}
	if existing, err := scanOrder(tx.QueryRowContext(c.Request.Context(), orderSelect+` WHERE o.user_id = $1 AND o.idempotency_key = $2`, userID, key)); err == nil {
		if err := loadOrderItems(c.Request.Context(), tx, &existing); err != nil {
			internalError(c)
			return
		}
		if err := tx.Commit(); err != nil {
			internalError(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": existing, "idempotent_replay": true})
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		internalError(c)
		return
	}
	rows, err := tx.QueryContext(c.Request.Context(), `SELECT p.id, p.name, p.slug, p.image_url, p.price, p.stock, p.is_active, ci.quantity
		FROM cart_items ci JOIN products p ON p.id = ci.product_id WHERE ci.cart_id = $1 ORDER BY p.id FOR UPDATE OF p`, cartID)
	if err != nil {
		internalError(c)
		return
	}
	items := make([]checkoutItem, 0)
	for rows.Next() {
		var item checkoutItem
		var active bool
		if err := rows.Scan(&item.productID, &item.name, &item.slug, &item.image, &item.price, &item.stock, &active, &item.quantity); err != nil {
			rows.Close()
			internalError(c)
			return
		}
		if !active {
			rows.Close()
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Sản phẩm %q không còn được bán", item.name)})
			return
		}
		if item.quantity > item.stock {
			rows.Close()
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Sản phẩm %q không đủ tồn kho", item.name)})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		internalError(c)
		return
	}
	rows.Close()
	if len(items) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Giỏ hàng đang trống"})
		return
	}
	var subtotalCents int64
	for _, item := range items {
		priceCents := int64(math.Round(item.price * 100))
		subtotalCents += priceCents * int64(item.quantity)
	}
	code, err := newOrderCode()
	if err != nil {
		internalError(c)
		return
	}
	amount := centsDecimal(subtotalCents)
	var orderID int64
	err = tx.QueryRowContext(c.Request.Context(), `INSERT INTO orders
		(code, user_id, email, recipient_name, phone, address, note, payment_method, payment_status, status, subtotal, shipping_fee, total, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'cod', 'unpaid', 'confirmed', $8, 0, $8, $9)
		RETURNING id`, code, userID, email, input.RecipientName, input.Phone, input.Address, input.Note, amount, key).Scan(&orderID)
	if err != nil {
		internalError(c)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `INSERT INTO order_status_history (order_id, from_status, to_status, changed_by) VALUES ($1, NULL, 'confirmed', $2)`, orderID, userID); err != nil {
		internalError(c)
		return
	}
	for _, item := range items {
		priceCents := int64(math.Round(item.price * 100))
		lineTotal := priceCents * int64(item.quantity)
		if _, err := tx.ExecContext(c.Request.Context(), `INSERT INTO order_items
			(order_id, product_id, product_name, product_slug, image_url, unit_price, quantity, line_total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, orderID, item.productID, item.name, item.slug, item.image, centsDecimal(priceCents), item.quantity, centsDecimal(lineTotal)); err != nil {
			internalError(c)
			return
		}
		if _, err := tx.ExecContext(c.Request.Context(), `UPDATE products SET stock = stock - $1, updated_at = NOW() WHERE id = $2 AND stock >= $1`, item.quantity, item.productID); err != nil {
			internalError(c)
			return
		}
		if _, err := tx.ExecContext(c.Request.Context(), `INSERT INTO inventory_movements (product_id, order_id, user_id, quantity_delta, reason) VALUES ($1, $2, $3, $4, 'order')`, item.productID, orderID, userID, -item.quantity); err != nil {
			internalError(c)
			return
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `INSERT INTO payments (order_id, provider, amount, status) VALUES ($1, 'cod', $2, 'pending')`, orderID, amount); err != nil {
		internalError(c)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM cart_items WHERE cart_id = $1`, cartID); err != nil {
		internalError(c)
		return
	}
	order, err := scanOrder(tx.QueryRowContext(c.Request.Context(), orderSelect+` WHERE o.id = $1`, orderID))
	if err != nil {
		internalError(c)
		return
	}
	if err := loadOrderItems(c.Request.Context(), tx, &order); err != nil {
		internalError(c)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": order, "idempotent_replay": false})
}

func centsDecimal(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

func newOrderCode() (string, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("LM-%s-%s", time.Now().Format("060102"), strings.ToUpper(hex.EncodeToString(random[:]))), nil
}

const orderSelect = `SELECT o.id, o.code, o.user_id, o.email, o.recipient_name, o.phone, o.address, o.note,
	o.payment_method, o.payment_status, o.status, o.subtotal, o.shipping_fee, o.total, o.created_at, o.updated_at FROM orders o`

type orderScanner interface{ Scan(...any) error }

func scanOrder(row orderScanner) (Order, error) {
	var order Order
	err := row.Scan(&order.ID, &order.Code, &order.UserID, &order.Email, &order.RecipientName, &order.Phone, &order.Address, &order.Note,
		&order.PaymentMethod, &order.PaymentStatus, &order.Status, &order.Subtotal, &order.ShippingFee, &order.Total, &order.CreatedAt, &order.UpdatedAt)
	return order, err
}

func loadOrderItems(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, order *Order) error {
	rows, err := queryer.QueryContext(ctx, `SELECT id, product_id, product_name, product_slug, image_url, unit_price, quantity, line_total
		FROM order_items WHERE order_id = $1 ORDER BY id`, order.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	order.Items = make([]OrderLine, 0)
	for rows.Next() {
		var item OrderLine
		if err := rows.Scan(&item.ID, &item.ProductID, &item.ProductName, &item.ProductSlug, &item.ImageURL, &item.UnitPrice, &item.Quantity, &item.LineTotal); err != nil {
			return err
		}
		order.Items = append(order.Items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	historyRows, err := queryer.QueryContext(ctx, `SELECT from_status, to_status, changed_by, created_at FROM order_status_history WHERE order_id = $1 ORDER BY created_at, id`, order.ID)
	if err != nil {
		return err
	}
	defer historyRows.Close()
	order.StatusHistory = make([]OrderStatusChange, 0)
	for historyRows.Next() {
		var change OrderStatusChange
		var from sql.NullString
		var changedBy sql.NullInt64
		if err := historyRows.Scan(&from, &change.ToStatus, &changedBy, &change.CreatedAt); err != nil {
			return err
		}
		if from.Valid {
			change.FromStatus = from.String
		}
		if changedBy.Valid {
			id := changedBy.Int64
			change.ChangedBy = &id
		}
		order.StatusHistory = append(order.StatusHistory, change)
	}
	return historyRows.Err()
}

func (orders *Orders) listOwnOrders(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	orders.listOrders(c, "o.user_id = $1", []any{userID})
}

func (orders *Orders) listAdminOrders(c *gin.Context) {
	page, limit, offset, ok := parsePage(c)
	if !ok {
		return
	}
	where := []string{"($1 = '' OR o.code ILIKE '%' || $1 || '%' OR o.email ILIKE '%' || $1 || '%' OR o.recipient_name ILIKE '%' || $1 || '%' OR o.phone ILIKE '%' || $1 || '%')"}
	args := []any{strings.TrimSpace(c.Query("q"))}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if !validOrderStatus(status) {
			badRequest(c, "status không hợp lệ")
			return
		}
		args = append(args, status)
		where = append(where, fmt.Sprintf("o.status = $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := orders.db.QueryRowContext(c.Request.Context(), `SELECT COUNT(*) FROM orders o WHERE `+whereSQL, args...).Scan(&total); err != nil {
		internalError(c)
		return
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	query := orderSelect + ` WHERE ` + whereSQL + fmt.Sprintf(" ORDER BY o.created_at DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	rows, err := orders.db.QueryContext(c.Request.Context(), query, queryArgs...)
	if err != nil {
		internalError(c)
		return
	}
	defer rows.Close()
	result := make([]Order, 0, limit)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			internalError(c)
			return
		}
		result = append(result, order)
	}
	if err := rows.Err(); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, pageResult[Order]{Data: result, Page: page, Limit: limit, Total: total})
}

func (orders *Orders) listOrders(c *gin.Context, where string, args []any) {
	page, limit, offset, ok := parsePage(c)
	if !ok {
		return
	}
	var total int64
	if err := orders.db.QueryRowContext(c.Request.Context(), `SELECT COUNT(*) FROM orders o WHERE `+where, args...).Scan(&total); err != nil {
		internalError(c)
		return
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	query := orderSelect + ` WHERE ` + where + fmt.Sprintf(" ORDER BY o.created_at DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	rows, err := orders.db.QueryContext(c.Request.Context(), query, queryArgs...)
	if err != nil {
		internalError(c)
		return
	}
	defer rows.Close()
	result := make([]Order, 0, limit)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			internalError(c)
			return
		}
		result = append(result, order)
	}
	if err := rows.Err(); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, pageResult[Order]{Data: result, Page: page, Limit: limit, Total: total})
}

func (orders *Orders) getOwnOrder(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	orders.getOrder(c, userID, false)
}

func (orders *Orders) getAdminOrder(c *gin.Context) { orders.getOrder(c, 0, true) }

func (orders *Orders) getOrder(c *gin.Context, userID int64, admin bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		badRequest(c, "ID đơn hàng không hợp lệ")
		return
	}
	query := orderSelect + ` WHERE o.id = $1`
	args := []any{id}
	if !admin {
		query += ` AND o.user_id = $2`
		args = append(args, userID)
	}
	order, err := scanOrder(orders.db.QueryRowContext(c.Request.Context(), query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy đơn hàng"})
		return
	}
	if err != nil {
		internalError(c)
		return
	}
	if err := loadOrderItems(c.Request.Context(), orders.db, &order); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": order})
}

func (orders *Orders) cancelOwnOrder(c *gin.Context) {
	userID, ok := authUserID(c)
	if !ok {
		return
	}
	order, err := orders.transition(c.Request.Context(), c.Param("id"), userID, false, "cancelled")
	if err != nil {
		writeOrderTransitionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": order})
}

func (orders *Orders) updateOrderStatus(c *gin.Context) {
	actorID, ok := authUserID(c)
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || !validOrderStatus(input.Status) {
		badRequest(c, "status không hợp lệ")
		return
	}
	order, err := orders.transition(c.Request.Context(), c.Param("id"), actorID, true, input.Status)
	if err != nil {
		writeOrderTransitionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": order})
}

func (orders *Orders) transition(ctx context.Context, rawID string, userID int64, admin bool, target string) (Order, error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		return Order{}, errOrderNotFound
	}
	tx, err := orders.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback()
	query := `SELECT status, payment_method FROM orders WHERE id = $1`
	args := []any{id}
	if !admin {
		query += ` AND user_id = $2`
		args = append(args, userID)
	}
	query += ` FOR UPDATE`
	var current, method string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&current, &method); errors.Is(err, sql.ErrNoRows) {
		return Order{}, errOrderNotFound
	} else if err != nil {
		return Order{}, err
	}
	if !canTransition(current, target, admin) {
		return Order{}, errOrderNotCancellable
	}
	if target == "cancelled" {
		rows, err := tx.QueryContext(ctx, `SELECT product_id, quantity FROM order_items WHERE order_id = $1`, id)
		if err != nil {
			return Order{}, err
		}
		type restoreLine struct {
			productID int64
			quantity  int
		}
		lines := make([]restoreLine, 0)
		for rows.Next() {
			var line restoreLine
			if err := rows.Scan(&line.productID, &line.quantity); err != nil {
				rows.Close()
				return Order{}, err
			}
			lines = append(lines, line)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return Order{}, err
		}
		rows.Close()
		for _, line := range lines {
			if _, err := tx.ExecContext(ctx, `UPDATE products SET stock = stock + $1, updated_at = NOW() WHERE id = $2`, line.quantity, line.productID); err != nil {
				return Order{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (product_id, order_id, user_id, quantity_delta, reason) VALUES ($1, $2, $3, $4, 'order_cancelled')`, line.productID, id, nullableUserID(userID), line.quantity); err != nil {
				return Order{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE payments SET status = 'cancelled', updated_at = NOW() WHERE order_id = $1 AND status = 'pending'`, id); err != nil {
			return Order{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'cancelled', payment_status = 'cancelled', updated_at = NOW() WHERE id = $1`, id); err != nil {
			return Order{}, err
		}
	} else {
		paymentStatus := "unpaid"
		if method == "cod" && target == "delivered" {
			paymentStatus = "paid"
			if _, err := tx.ExecContext(ctx, `UPDATE payments SET status = 'succeeded', paid_at = NOW(), updated_at = NOW() WHERE order_id = $1 AND status = 'pending'`, id); err != nil {
				return Order{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = $1, payment_status = $2, updated_at = NOW() WHERE id = $3`, target, paymentStatus, id); err != nil {
			return Order{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO order_status_history (order_id, from_status, to_status, changed_by) VALUES ($1, $2, $3, $4)`, id, current, target, nullableUserID(userID)); err != nil {
		return Order{}, err
	}
	order, err := scanOrder(tx.QueryRowContext(ctx, orderSelect+` WHERE o.id = $1`, id))
	if err != nil {
		return Order{}, err
	}
	if err := loadOrderItems(ctx, tx, &order); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, err
	}
	return order, nil
}

func nullableUserID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func canTransition(current, target string, admin bool) bool {
	if target == "cancelled" {
		if admin {
			return current == "confirmed" || current == "processing"
		}
		return current == "confirmed"
	}
	if !admin {
		return false
	}
	switch current {
	case "confirmed":
		return target == "processing"
	case "processing":
		return target == "shipped"
	case "shipped":
		return target == "delivered"
	default:
		return false
	}
}

func validOrderStatus(status string) bool {
	switch status {
	case "confirmed", "processing", "shipped", "delivered", "cancelled":
		return true
	default:
		return false
	}
}

func writeOrderTransitionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errOrderNotFound), errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy đơn hàng"})
	case errors.Is(err, errOrderNotCancellable):
		c.JSON(http.StatusConflict, gin.H{"error": "Không thể chuyển đơn hàng từ trạng thái hiện tại"})
	default:
		internalError(c)
	}
}
