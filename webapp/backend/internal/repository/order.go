package repository

import (
	"backend/internal/model"
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

type OrderRepository struct {
	db DBTX
}

func NewOrderRepository(db DBTX) *OrderRepository {
	return &OrderRepository{db: db}
}

// 注文を作成し、生成された注文IDを返す
func (r *OrderRepository) Create(ctx context.Context, order *model.Order) (string, error) {
	query := `INSERT INTO orders (user_id, product_id, shipped_status, created_at) VALUES (?, ?, 'shipping', NOW())`
	result, err := r.db.ExecContext(ctx, query, order.UserID, order.ProductID)
	if err != nil {
		return "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}

// 複数の注文IDのステータスを一括で更新
// 主に配送ロボットが注文を引き受けた際に一括更新をするために使用
func (r *OrderRepository) UpdateStatuses(ctx context.Context, orderIDs []int64, newStatus string) error {
	if len(orderIDs) == 0 {
		return nil
	}
	query, args, err := sqlx.In("UPDATE orders SET shipped_status = ? WHERE order_id IN (?)", newStatus, orderIDs)
	if err != nil {
		return err
	}
	query = r.db.Rebind(query)
	_, err = r.db.ExecContext(ctx, query, args...)
	return err
}

// UpdateStatusesIfCurrentStatus
// 仅当当前状态等于 fromStatus 时，才更新为 newStatus。
// 返回实际被更新的行数，用于并发场景下做乐观锁控制。
func (r *OrderRepository) UpdateStatusesIfCurrentStatus(
    ctx context.Context,
    orderIDs []int64,
    fromStatus string,
    newStatus string,
) (int64, error) {
    if len(orderIDs) == 0 {
        return 0, nil
    }

    // 使用 sqlx.In 展开 IN (...)
    query, args, err := sqlx.In(
        `UPDATE orders
         SET shipped_status = ?
         WHERE shipped_status = ?
           AND order_id IN (?)`,
        newStatus,
        fromStatus,
        orderIDs,
    )
    if err != nil {
        return 0, err
    }

    query = r.db.Rebind(query)
    res, err := r.db.ExecContext(ctx, query, args...)
    if err != nil {
        return 0, err
    }

    rows, err := res.RowsAffected()
    if err != nil {
        return 0, err
    }
    return rows, nil
}

// 配送中(shipped_status:shipping)の注文一覧を取得
func (r *OrderRepository) GetShippingOrders(ctx context.Context) ([]model.Order, error) {
	var orders []model.Order
	query := `
        SELECT
            o.order_id,
            p.weight,
            p.value
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.shipped_status = 'shipping'
    `
	err := r.db.SelectContext(ctx, &orders, query)
	return orders, err
}


// 注文履歴一覧を取得 (Optimized)
func (r *OrderRepository) ListOrders(ctx context.Context, userID int, req model.ListRequest) ([]model.Order, int, error) {
	// 1. 先获取总数 (変更なし)
	total, err := r.GetTotalOrdersCount(ctx, userID, req)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.Order{}, 0, nil
	}

	// 2. 构建主查询 (変更なし)
	queryArgs := []interface{}{userID}
	var queryBuilder strings.Builder
	queryBuilder.WriteString(`
        SELECT
            o.order_id,
            o.product_id,
            p.name AS product_name,
            o.shipped_status,
            o.created_at,
            o.arrived_at
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.user_id = ?
    `)

	// 添加搜索条件 (変更なし)
	if req.Search != "" {
		searchPattern := ""
		if req.Type == "prefix" {
			searchPattern = req.Search + "%"
		} else {
			searchPattern = "%" + req.Search + "%"
		}
		queryBuilder.WriteString(" AND p.name LIKE ?")
		queryArgs = append(queryArgs, searchPattern)
	}

	// 添加排序 (変更なし)
	sortField := "o.order_id" // デフォルト値
	switch req.SortField {
	case "product_name":
		sortField = "p.name"
	case "created_at":
		sortField = "o.created_at"
	case "shipped_status":
		sortField = "o.shipped_status"
	case "arrived_at":
		sortField = "o.arrived_at"
	}
	sortOrder := "DESC"
	if strings.ToUpper(req.SortOrder) == "ASC" {
		sortOrder = "ASC"
	}
	queryBuilder.WriteString(fmt.Sprintf(" ORDER BY %s %s, o.order_id ASC", sortField, sortOrder))

	// 添加分页 (変更なし)
	queryBuilder.WriteString(" LIMIT ? OFFSET ?")
	queryArgs = append(queryArgs, req.PageSize, req.Offset)


	// 3. 执行查询
	
	// 修正： 'orderRow' の定義を、元のコードに合わせて明示的にする
	type orderRow struct {
		OrderID       int64          `db:"order_id"`
		ProductID     int            `db:"product_id"`
		ProductName   sql.NullString `db:"product_name"` // p.name をマッピング
		ShippedStatus string         `db:"shipped_status"`
		CreatedAt     sql.NullTime   `db:"created_at"`
		ArrivedAt     sql.NullTime   `db:"arrived_at"`
	}
	var ordersRaw []orderRow

	if err := r.db.SelectContext(ctx, &ordersRaw, queryBuilder.String(), queryArgs...); err != nil {
        return nil, 0, err
    }

    // 4. 转换数据
	// 修正： 'orderRow' から 'model.Order' へ手動でマッピングする
	var orders []model.Order
    orders = make([]model.Order, len(ordersRaw))
    for i, raw := range ordersRaw {
        orders[i] = model.Order{
            OrderID:       raw.OrderID,
            ProductID:     raw.ProductID,
            ProductName:   raw.ProductName.String, // sql.NullString から string へ
            ShippedStatus: raw.ShippedStatus,
            CreatedAt:     raw.CreatedAt.Time,   // sql.NullTime から time.Time へ
            ArrivedAt:     raw.ArrivedAt,      // ArrivedAt は sql.NullTime のまま
        }
    }

	// 5. 返回分页结果和总数
	return orders, total, nil
}

// GetTotalOrdersCount 获取筛选后的订单总数
func (r *OrderRepository) GetTotalOrdersCount(ctx context.Context, userID int, req model.ListRequest) (int, error) {
	var total int
	queryArgs := []interface{}{userID}

	var queryBuilder strings.Builder
	queryBuilder.WriteString(`
        SELECT COUNT(o.order_id)
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.user_id = ?
    `)

	if req.Search != "" {
		searchPattern := ""
		if req.Type == "prefix" {
			searchPattern = req.Search + "%"
		} else {
			searchPattern = "%" + req.Search + "%"
		}
		queryBuilder.WriteString(" AND p.name LIKE ?")
		queryArgs = append(queryArgs, searchPattern)
	}

	err := r.db.GetContext(ctx, &total, queryBuilder.String(), queryArgs...)
	return total, err
}
