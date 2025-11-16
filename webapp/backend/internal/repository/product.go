package repository

import (
	"backend/internal/model"
	"context"
	"fmt"
	"strings"
)

type ProductRepository struct {
	db DBTX
}

func NewProductRepository(db DBTX) *ProductRepository {
	return &ProductRepository{db: db}
}

// GetTotalProductsCount 获取筛选后的商品总数
func (r *ProductRepository) GetTotalProductsCount(ctx context.Context, req model.ListRequest) (int, error) {
    var total int
    // 使用 strings.Builder
    var queryBuilder strings.Builder
    queryBuilder.WriteString("SELECT COUNT(product_id) FROM products")

    args := []interface{}{}

    if req.Search != "" {
        queryBuilder.WriteString(" WHERE (name LIKE ? OR description LIKE ?)")
        searchPattern := "%" + req.Search + "%"
        args = append(args, searchPattern, searchPattern)
    }

    err := r.db.GetContext(ctx, &total, queryBuilder.String(), args...) // 使用 queryBuilder.String()
    return total, err
}

// 商品一覧を取得 (Optimized)
func (r *ProductRepository) ListProducts(ctx context.Context, userID int, req model.ListRequest) ([]model.Product, int, error) {
	// 1. 获取总数
	total, err := r.GetTotalProductsCount(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.Product{}, 0, nil
	}

	// 2. 构建主查询
	var products []model.Product
    // 使用 strings.Builder
    var queryBuilder strings.Builder
    queryBuilder.WriteString(`
        SELECT product_id, name, value, weight, image, description
        FROM products
    `)

    args := []interface{}{}

    if req.Search != "" {
        queryBuilder.WriteString(" WHERE (name LIKE ? OR description LIKE ?)")
        searchPattern := "%" + req.Search + "%"
        args = append(args, searchPattern, searchPattern)
    }

	// 3. 添加排序 (ここは変更なし・最適化済み)
	// 安全校验：防止SQL注入
	sortField := "product_id" 
    switch req.SortField {
    case "name":
        sortField = "name"
    case "value":
        sortField = "value"
    case "weight":
        sortField = "weight"
    }
    sortOrder := "ASC"
    if strings.ToUpper(req.SortOrder) == "DESC" {
        sortOrder = "DESC"
    }
    // 但为了统一，我们也可以用 WriteString
    queryBuilder.WriteString(fmt.Sprintf(" ORDER BY %s %s, product_id ASC", sortField, sortOrder))

	// 4. 添加分页 (ここは変更なし・最適化済み)
	queryBuilder.WriteString(" LIMIT ? OFFSET ?")
	args = append(args, req.PageSize, req.Offset)

	err = r.db.SelectContext(ctx, &products, queryBuilder.String(), args...)
	if err != nil {
		return nil, 0, err
	}

	// 5. 返回分页结果和总数
	return products, total, nil
}