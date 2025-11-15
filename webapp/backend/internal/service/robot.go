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
		return s.store.ExecTx(ctx, func(txStore *repository.Store) error {
			orders, err := txStore.OrderRepo.GetShippingOrders(ctx)
			if err != nil {
				return err
			}
			plan, err = selectOrdersForDelivery(ctx, orders, robotID, capacity)
			if err != nil {
				return err
			}
			if len(plan.Orders) > 0 {
				orderIDs := make([]int64, len(plan.Orders))
				for i, order := range plan.Orders {
					orderIDs[i] = order.OrderID
				}

				if err := txStore.OrderRepo.UpdateStatuses(ctx, orderIDs, "delivering"); err != nil {
					return err
				}
				log.Printf("Updated status to 'delivering' for %d orders", len(orderIDs))
			}
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
// This function solves the 0/1 Knapsack Problem efficiently.
func selectOrdersForDelivery(ctx context.Context, orders []model.Order, robotID string, robotCapacity int) (model.DeliveryPlan, error) {
    n := len(orders)
    if n == 0 {
        return model.DeliveryPlan{RobotID: robotID}, nil
    }

    // dp[i][w] stores the maximum value using the first 'i' items with a weight limit of 'w'.
    // We use n+1 and robotCapacity+1 to handle 1-based indexing easily.
    dp := make([][]int, n+1)
    
    // 'choice[i][w]' tracks whether we *included* item 'i' to get the max value at dp[i][w].
    // This is necessary to reconstruct the list of orders.
    choice := make([][]bool, n+1)

    for i := 0; i <= n; i++ {
        dp[i] = make([]int, robotCapacity+1)
        choice[i] = make([]bool, robotCapacity+1)
    }

    // --- 1. Building the DP Table ---
    // Iterate through each order (item)
    for i := 1; i <= n; i++ {
        // Get the actual order details. (i-1 because 'orders' is 0-indexed)
        order := orders[i-1]
        weight := order.Weight
        value := order.Value
        
        // Check for context cancellation every 'n' items (can be adjusted)
        if i % 1024 == 0 {
             select {
            case <-ctx.Done():
                return model.DeliveryPlan{}, ctx.Err()
            default:
            }
        }

        // Iterate through each possible capacity 'w'
        for w := 0; w <= robotCapacity; w++ {
            
            // Option 1: Don't include the current item 'i'.
            // The value is the same as the best value without this item.
            dp[i][w] = dp[i-1][w]
            choice[i][w] = false // Mark as 'not taken'

            // Option 2: Include the current item 'i' (if it fits)
            if w >= weight {
                // Value if we *do* take this item
                valueWithItem := dp[i-1][w-weight] + value

                // If taking the item gives a better value, update dp and 'choice'
                if valueWithItem > dp[i][w] {
                    dp[i][w] = valueWithItem
                    choice[i][w] = true // Mark as 'taken'
                }
            }
        }
    }

    // The best possible value is now in dp[n][robotCapacity]
    bestValue := dp[n][robotCapacity]
    
    // --- 2. Backtracking to find the selected orders ---
    var bestSet []model.Order
    totalWeight := 0
    curCap := robotCapacity // Start from the full capacity

    for i := n; i > 0; i-- {
        // Check if we decided to take item 'i' at this capacity
        if choice[i][curCap] {
            order := orders[i-1]
            bestSet = append(bestSet, order)
            totalWeight += order.Weight
            curCap -= order.Weight // Reduce capacity for the next check
        }
    }
    
    // Note: 'bestSet' will be in reverse order of processing, 
    // but the order of items in the plan doesn't matter.

    return model.DeliveryPlan{
        RobotID:     robotID,
        TotalWeight: totalWeight,
        TotalValue:  bestValue,
        Orders:      bestSet,
    }, nil
}