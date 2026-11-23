package http

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/text/unicode/norm"
)

type Catalog struct {
	db        *sql.DB
	uploadDir string
}

type Category struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Product struct {
	ID          int64     `json:"id"`
	CategoryID  int64     `json:"category_id"`
	Category    string    `json:"category"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Price       float64   `json:"price"`
	Stock       int       `json:"stock"`
	ImageURL    string    `json:"image_url"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type pageResult[T any] struct {
	Data  []T   `json:"data"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}

func NewCatalog(db *sql.DB, uploadDir string) *Catalog { return &Catalog{db: db, uploadDir: uploadDir} }

func (catalog *Catalog) EnsureSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS categories (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(120) NOT NULL UNIQUE,
			slug VARCHAR(140) NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS products (
			id BIGSERIAL PRIMARY KEY,
			category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
			name VARCHAR(180) NOT NULL,
			slug VARCHAR(200) NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			price NUMERIC(12,2) NOT NULL CHECK (price >= 0),
			stock INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
			image_url TEXT NOT NULL DEFAULT '',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS products_category_id_idx ON products(category_id)`,
		`CREATE INDEX IF NOT EXISTS products_created_at_idx ON products(created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := catalog.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (catalog *Catalog) RegisterRoutes(api *gin.RouterGroup, auth *Auth) {
	api.GET("/categories", catalog.listCategories)
	api.GET("/categories/:id", catalog.getCategory)
	api.GET("/products", catalog.listProducts(false))
	api.GET("/products/:id", catalog.getProduct)

	admin := api.Group("/admin", auth.RequireAuth(), auth.RequireRole("admin"))
	admin.GET("/categories", catalog.listCategories)
	admin.POST("/categories", catalog.createCategory)
	admin.PUT("/categories/:id", catalog.updateCategory)
	admin.DELETE("/categories/:id", catalog.deleteCategory)
	admin.GET("/products", catalog.listProducts(true))
	admin.POST("/products", catalog.createProduct)
	admin.PUT("/products/:id", catalog.updateProduct)
	admin.DELETE("/products/:id", catalog.deleteProduct)
	admin.POST("/uploads", catalog.uploadImage)
}

func (catalog *Catalog) listCategories(c *gin.Context) {
	rows, err := catalog.db.QueryContext(c.Request.Context(), `
		SELECT id, name, slug, description, created_at, updated_at
		FROM categories ORDER BY name ASC`)
	if err != nil {
		internalError(c)
		return
	}
	defer rows.Close()
	categories := make([]Category, 0)
	for rows.Next() {
		var category Category
		if err := rows.Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.CreatedAt, &category.UpdatedAt); err != nil {
			internalError(c)
			return
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": categories})
}

func (catalog *Catalog) getCategory(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var category Category
	err := catalog.db.QueryRowContext(c.Request.Context(), `SELECT id, name, slug, description, created_at, updated_at FROM categories WHERE id = $1`, id).
		Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.CreatedAt, &category.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}
	if err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": category})
}

type categoryInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

func (input *categoryInput) normalize() error {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = normalizeSlug(input.Slug)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || len(input.Name) > 120 {
		return errors.New("Tên danh mục bắt buộc và tối đa 120 ký tự")
	}
	if input.Slug == "" {
		input.Slug = normalizeSlug(input.Name)
	}
	if input.Slug == "" || len(input.Slug) > 140 {
		return errors.New("Slug danh mục không hợp lệ")
	}
	return nil
}

func (catalog *Catalog) createCategory(c *gin.Context) {
	var input categoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, "Dữ liệu danh mục không hợp lệ")
		return
	}
	if err := input.normalize(); err != nil {
		badRequest(c, err.Error())
		return
	}
	var category Category
	err := catalog.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO categories (name, slug, description) VALUES ($1, $2, $3)
		RETURNING id, name, slug, description, created_at, updated_at`, input.Name, input.Slug, input.Description).
		Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.CreatedAt, &category.UpdatedAt)
	if err != nil {
		writeCatalogDBError(c, err, "Không thể tạo danh mục")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": category})
}

func (catalog *Catalog) updateCategory(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input categoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, "Dữ liệu danh mục không hợp lệ")
		return
	}
	if err := input.normalize(); err != nil {
		badRequest(c, err.Error())
		return
	}
	var category Category
	err := catalog.db.QueryRowContext(c.Request.Context(), `
		UPDATE categories SET name = $1, slug = $2, description = $3, updated_at = NOW()
		WHERE id = $4 RETURNING id, name, slug, description, created_at, updated_at`, input.Name, input.Slug, input.Description, id).
		Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.CreatedAt, &category.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}
	if err != nil {
		writeCatalogDBError(c, err, "Không thể cập nhật danh mục")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": category})
}

func (catalog *Catalog) deleteCategory(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	result, err := catalog.db.ExecContext(c.Request.Context(), `DELETE FROM categories WHERE id = $1`, id)
	if err != nil {
		writeCatalogDBError(c, err, "Không thể xóa danh mục đang được sản phẩm sử dụng")
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (catalog *Catalog) listProducts(includeInactive bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, limit, offset, ok := parsePage(c)
		if !ok {
			return
		}
		where := []string{"($1 = '' OR p.name ILIKE '%' || $1 || '%' OR p.description ILIKE '%' || $1 || '%' OR c.name ILIKE '%' || $1 || '%')"}
		args := []any{strings.TrimSpace(c.Query("q"))}
		if !includeInactive {
			where = append(where, "p.is_active = TRUE")
		}
		if raw := c.Query("category_id"); raw != "" {
			categoryID, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || categoryID < 1 {
				badRequest(c, "category_id không hợp lệ")
				return
			}
			args = append(args, categoryID)
			where = append(where, fmt.Sprintf("p.category_id = $%d", len(args)))
		}
		for _, filter := range []struct {
			query  string
			column string
			min    bool
		}{{"min_price", "p.price", true}, {"max_price", "p.price", false}} {
			if raw := c.Query(filter.query); raw != "" {
				price, err := strconv.ParseFloat(raw, 64)
				if err != nil || price < 0 || math.IsNaN(price) || math.IsInf(price, 0) {
					badRequest(c, filter.query+" phải là số không âm")
					return
				}
				args = append(args, price)
				op := "<="
				if filter.min {
					op = ">="
				}
				where = append(where, fmt.Sprintf("%s %s $%d", filter.column, op, len(args)))
			}
		}
		whereSQL := strings.Join(where, " AND ")
		var total int64
		countQuery := `SELECT COUNT(*) FROM products p JOIN categories c ON c.id = p.category_id WHERE ` + whereSQL
		if err := catalog.db.QueryRowContext(c.Request.Context(), countQuery, args...).Scan(&total); err != nil {
			internalError(c)
			return
		}
		sortColumns := map[string]string{"name": "p.name", "price": "p.price", "created_at": "p.created_at"}
		sortColumn, valid := sortColumns[c.DefaultQuery("sort_by", "created_at")]
		if !valid {
			badRequest(c, "sort_by nhận name, price hoặc created_at")
			return
		}
		direction := strings.ToUpper(c.DefaultQuery("sort_order", "desc"))
		if direction != "ASC" && direction != "DESC" {
			badRequest(c, "sort_order nhận asc hoặc desc")
			return
		}
		queryArgs := append(append([]any{}, args...), limit, offset)
		query := `SELECT p.id, p.category_id, c.name, p.name, p.slug, p.description, p.price, p.stock, p.image_url, p.is_active, p.created_at, p.updated_at
			FROM products p JOIN categories c ON c.id = p.category_id WHERE ` + whereSQL +
			fmt.Sprintf(" ORDER BY %s %s, p.id ASC LIMIT $%d OFFSET $%d", sortColumn, direction, len(args)+1, len(args)+2)
		rows, err := catalog.db.QueryContext(c.Request.Context(), query, queryArgs...)
		if err != nil {
			internalError(c)
			return
		}
		defer rows.Close()
		products := make([]Product, 0, limit)
		for rows.Next() {
			var product Product
			if err := rows.Scan(&product.ID, &product.CategoryID, &product.Category, &product.Name, &product.Slug, &product.Description, &product.Price, &product.Stock, &product.ImageURL, &product.IsActive, &product.CreatedAt, &product.UpdatedAt); err != nil {
				internalError(c)
				return
			}
			products = append(products, product)
		}
		if err := rows.Err(); err != nil {
			internalError(c)
			return
		}
		c.JSON(http.StatusOK, pageResult[Product]{Data: products, Page: page, Limit: limit, Total: total})
	}
}

func (catalog *Catalog) getProduct(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	product, err := catalog.findProduct(c, id, false)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	if err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": product})
}

type productInput struct {
	CategoryID  int64   `json:"category_id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description"`
	Price       float64 `json:"price"`
	Stock       int     `json:"stock"`
	ImageURL    string  `json:"image_url"`
	IsActive    *bool   `json:"is_active"`
}

func (input *productInput) normalize() error {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = normalizeSlug(input.Slug)
	input.Description = strings.TrimSpace(input.Description)
	input.ImageURL = strings.TrimSpace(input.ImageURL)
	if input.CategoryID < 1 || input.Name == "" || len(input.Name) > 180 {
		return errors.New("Tên và danh mục sản phẩm là bắt buộc")
	}
	if input.Slug == "" {
		input.Slug = normalizeSlug(input.Name)
	}
	if input.Slug == "" || len(input.Slug) > 200 {
		return errors.New("Slug sản phẩm không hợp lệ")
	}
	if input.Price < 0 || input.Price > 9999999999.99 || input.Stock < 0 {
		return errors.New("Giá và tồn kho phải là số không âm")
	}
	if input.ImageURL != "" && !strings.HasPrefix(input.ImageURL, "/uploads/") {
		return errors.New("Ảnh sản phẩm phải được tải lên hệ thống trước")
	}
	if input.IsActive == nil {
		active := true
		input.IsActive = &active
	}
	return nil
}

func (catalog *Catalog) createProduct(c *gin.Context) {
	var input productInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, "Dữ liệu sản phẩm không hợp lệ")
		return
	}
	if err := input.normalize(); err != nil {
		badRequest(c, err.Error())
		return
	}
	product, err := catalog.saveProduct(c, 0, input)
	if err != nil {
		writeCatalogDBError(c, err, "Không thể tạo sản phẩm")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": product})
}

func (catalog *Catalog) updateProduct(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input productInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, "Dữ liệu sản phẩm không hợp lệ")
		return
	}
	if err := input.normalize(); err != nil {
		badRequest(c, err.Error())
		return
	}
	previous, err := catalog.findProduct(c, id, true)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	if err != nil {
		internalError(c)
		return
	}
	product, err := catalog.saveProduct(c, id, input)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	if err != nil {
		writeCatalogDBError(c, err, "Không thể cập nhật sản phẩm")
		return
	}
	if previous.ImageURL != product.ImageURL {
		catalog.removeUnusedImage(previous.ImageURL)
	}
	c.JSON(http.StatusOK, gin.H{"data": product})
}

func (catalog *Catalog) saveProduct(c *gin.Context, id int64, input productInput) (Product, error) {
	var product Product
	query := `INSERT INTO products (category_id, name, slug, description, price, stock, image_url, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, category_id, name, slug, description, price, stock, image_url, is_active, created_at, updated_at`
	args := []any{input.CategoryID, input.Name, input.Slug, input.Description, input.Price, input.Stock, input.ImageURL, *input.IsActive}
	if id > 0 {
		query = `UPDATE products SET category_id = $1, name = $2, slug = $3, description = $4, price = $5, stock = $6,
			image_url = $7, is_active = $8, updated_at = NOW() WHERE id = $9
			RETURNING id, category_id, name, slug, description, price, stock, image_url, is_active, created_at, updated_at`
		args = append(args, id)
	}
	err := catalog.db.QueryRowContext(c.Request.Context(), query, args...).Scan(
		&product.ID, &product.CategoryID, &product.Name, &product.Slug, &product.Description, &product.Price, &product.Stock, &product.ImageURL, &product.IsActive, &product.CreatedAt, &product.UpdatedAt)
	if err != nil {
		return Product{}, err
	}
	err = catalog.db.QueryRowContext(c.Request.Context(), `SELECT name FROM categories WHERE id = $1`, product.CategoryID).Scan(&product.Category)
	return product, err
}

func (catalog *Catalog) deleteProduct(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var imageURL string
	err := catalog.db.QueryRowContext(c.Request.Context(), `SELECT image_url FROM products WHERE id = $1`, id).Scan(&imageURL)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	if err != nil {
		internalError(c)
		return
	}
	result, err := catalog.db.ExecContext(c.Request.Context(), `DELETE FROM products WHERE id = $1`, id)
	if err != nil {
		internalError(c)
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	catalog.removeUnusedImage(imageURL)
	c.Status(http.StatusNoContent)
}

func (catalog *Catalog) removeUnusedImage(imageURL string) {
	if !strings.HasPrefix(imageURL, "/uploads/") {
		return
	}
	filename := strings.TrimPrefix(imageURL, "/uploads/")
	if filename == "" || filepath.Base(filename) != filename {
		return
	}
	var references int
	if err := catalog.db.QueryRow(`SELECT COUNT(*) FROM products WHERE image_url = $1`, imageURL).Scan(&references); err == nil && references == 0 {
		_ = os.Remove(filepath.Join(catalog.uploadDir, filename))
	}
}

func (catalog *Catalog) findProduct(c *gin.Context, id int64, includeInactive bool) (Product, error) {
	var product Product
	query := `SELECT p.id, p.category_id, c.name, p.name, p.slug, p.description, p.price, p.stock, p.image_url, p.is_active, p.created_at, p.updated_at
		FROM products p JOIN categories c ON c.id = p.category_id WHERE p.id = $1`
	if !includeInactive {
		query += ` AND p.is_active = TRUE`
	}
	err := catalog.db.QueryRowContext(c.Request.Context(), query, id).Scan(
		&product.ID, &product.CategoryID, &product.Category, &product.Name, &product.Slug, &product.Description, &product.Price, &product.Stock, &product.ImageURL, &product.IsActive, &product.CreatedAt, &product.UpdatedAt)
	return product, err
}

func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		badRequest(c, "ID không hợp lệ")
		return 0, false
	}
	return id, true
}

func parsePage(c *gin.Context) (int, int, int, bool) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 1000000000 {
		badRequest(c, "page phải là số nguyên dương")
		return 0, 0, 0, false
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "12"))
	if err != nil || limit < 1 || limit > 100 {
		badRequest(c, "limit phải nằm trong khoảng 1 đến 100")
		return 0, 0, 0, false
	}
	return page, limit, (page - 1) * limit, true
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "đ", "d")
	value = norm.NFD.String(value)
	var builder strings.Builder
	separator := false
	for _, char := range value {
		if unicode.Is(unicode.Mn, char) {
			continue
		}
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			builder.WriteRune(char)
			separator = false
		} else if !separator && builder.Len() > 0 {
			builder.WriteByte('-')
			separator = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func badRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": message})
}

func internalError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Lỗi máy chủ"})
}

func writeCatalogDBError(c *gin.Context, err error, fallback string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			c.JSON(http.StatusConflict, gin.H{"error": "Tên hoặc slug đã tồn tại"})
			return
		case "23503":
			c.JSON(http.StatusConflict, gin.H{"error": "Danh mục không tồn tại hoặc vẫn còn sản phẩm thuộc danh mục"})
			return
		case "23514":
			badRequest(c, "Giá trị không hợp lệ")
			return
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy dữ liệu"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": fallback})
}
