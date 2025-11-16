package service

import (
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/service/utils"
	"context"
	"log"
)

type RobotService struct {
	store *repository.Store
}

func NewRobotService(store *repository.Store) *RobotService {
	return &RobotService{store: store}
}

// 注意：このメソッドは、現在、ordersテーブルのshipped_statusが"shipping"になっている注文"全件"を対象に配送計画を立てます。
// 注文の取得件数を制限した場合、ペナルティの対象になります。
func (s *RobotService) GenerateDeliveryPlan(ctx context.Context, robotID string, capacity int) (*model.DeliveryPlan, error) {
    var plan model.DeliveryPlan

    err := utils.WithTimeout(ctx, func(ctx context.Context) error {
        // 1. 事务外读取 & 计算（纯 CPU 操作 + 一次 SELECT）
        orders, err := s.store.OrderRepo.GetShippingOrders(ctx)
        if err != nil {
            return err
        }

        localPlan, err := selectOrdersForDelivery(ctx, orders, robotID, capacity)
        if err != nil {
            return err
        }
        plan = localPlan

        if len(plan.Orders) == 0 {
            // 没有要配送的订单，直接返回，不开启事务
            return nil
        }

        orderIDs := make([]int64, len(plan.Orders))
        for i, order := range plan.Orders {
            orderIDs[i] = order.OrderID
        }

        // 2. 事务内只做一次短 UPDATE（乐观锁：仅更新当前仍为 shipping 的订单）
        return s.store.ExecTx(ctx, func(txStore *repository.Store) error {
            rows, err := txStore.OrderRepo.UpdateStatusesIfCurrentStatus(
                ctx,
                orderIDs,
                "shipping",   // fromStatus
                "delivering", // newStatus
            )
            if err != nil {
                return err
            }

            log.Printf("Updated status from 'shipping' to 'delivering' for %d/%d orders", rows, len(orderIDs))
            return nil
        })
    })
    if err != nil {
        return nil, err
    }
    return &plan, nil
}


func (s *RobotService) UpdateOrderStatus(ctx context.Context, orderID int64, newStatus string) error {
	return utils.WithTimeout(ctx, func(ctx context.Context) error {
		return s.store.OrderRepo.UpdateStatuses(ctx, []int64{orderID}, newStatus)
	})
}

// selectOrdersForDelivery (Optimized with Dynamic Programming)
func selectOrdersForDelivery(ctx context.Context, orders []model.Order, robotID string, robotCapacity int) (model.DeliveryPlan, error) {
    // --- 0. trivial cases ---
    if robotCapacity <= 0 || len(orders) == 0 {
        return model.DeliveryPlan{RobotID: robotID}, nil
    }

    // --- 1. 预过滤：去掉根本不可能装上的货 & 没价值的货 ---
    filtered := make([]model.Order, 0, len(orders))
    totalWeight := 0
    totalValue := 0

    for _, o := range orders {
        // weight <= 0 或 value <= 0 的货对得分没有帮助，直接跳过
        if o.Weight <= 0 || o.Value <= 0 {
            continue
        }
        // 比机器人容量还重的货，不可能选中，也跳过
        if o.Weight > robotCapacity {
            continue
        }

        filtered = append(filtered, o)
        totalWeight += o.Weight
        totalValue += o.Value
    }

    orders = filtered
    if len(orders) == 0 {
        // 过滤完啥也没有了
        return model.DeliveryPlan{RobotID: robotID}, nil
    }

    // --- 2. 早退出：所有货物总重本来就 <= 容量，直接全装上 ---
    if totalWeight <= robotCapacity {
        return model.DeliveryPlan{
            RobotID:     robotID,
            TotalWeight: totalWeight,
            TotalValue:  totalValue,
            Orders:      orders,
        }, nil
    }

    n := len(orders)

    // --- 3. DP table ---
    // dp[i][w] = 使用前 i 个货物、容量 w 时能得到的最大 value
    dp := make([][]int, n+1)
    // choice[i][w] = 在得到 dp[i][w] 的时候，第 i 个货物是否被选中
    choice := make([][]bool, n+1)

    for i := 0; i <= n; i++ {
        dp[i] = make([]int, robotCapacity+1)
        choice[i] = make([]bool, robotCapacity+1)
    }

    // --- 4. 填 DP 表 ---
    for i := 1; i <= n; i++ {
        ord := orders[i-1]
        w := ord.Weight
        v := ord.Value

        // 每隔一段检查一下 context，防止极端情况下超时
        if i%512 == 0 {
            select {
            case <-ctx.Done():
                return model.DeliveryPlan{}, ctx.Err()
            default:
            }
        }

        for c := 0; c <= robotCapacity; c++ {
            // 方案 A: 不选第 i 个
            best := dp[i-1][c]

            // 方案 B: 选第 i 个（前提是装得下）
            take := -1
            if w <= c {
                take = dp[i-1][c-w] + v
            }

            if take > best {
                dp[i][c] = take
                choice[i][c] = true
            } else {
                dp[i][c] = best
                choice[i][c] = false
            }
        }
    }

    // --- 5. 反向回溯出被选中的订单 ---
    capLeft := robotCapacity
    bestValue := dp[n][capLeft]
    selected := make([]model.Order, 0, n)
    totalSelectedWeight := 0

    for i := n; i >= 1; i-- {
        if capLeft <= 0 {
            break
        }
        if !choice[i][capLeft] {
            continue
        }
        ord := orders[i-1]
        selected = append(selected, ord)
        totalSelectedWeight += ord.Weight
        capLeft -= ord.Weight
    }

    return model.DeliveryPlan{
        RobotID:     robotID,
        TotalWeight: totalSelectedWeight,
        TotalValue:  bestValue,
        Orders:      selected,
    }, nil
}
