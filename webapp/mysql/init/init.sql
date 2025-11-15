USE `hiroshimauniv2511-db`;

DROP TABLE IF EXISTS user_sessions;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS `users`;

CREATE TABLE `users` (
  `user_id` INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  `password_hash` VARCHAR(255) NOT NULL,
  `user_name` VARCHAR(255) NOT NULL
  );

-- productsテーブルの作成
-- productsテーブルの作成
CREATE TABLE products (
    product_id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    value INT UNSIGNED NOT NULL,
    weight INT UNSIGNED NOT NULL,
    image VARCHAR(500),
    description TEXT,
    
    -- インデックスをテーブル定義内に記述
    INDEX idx_products_name (name(191)),
    INDEX idx_products_value (value),
    INDEX idx_products_weight (weight)
    
) ENGINE=InnoDB
DEFAULT CHARSET=utf8mb4
COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE orders (
    order_id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id INT UNSIGNED NOT NULL,
    product_id INT UNSIGNED NOT NULL,
    shipped_status VARCHAR(50) NOT NULL,
    created_at DATETIME NOT NULL,
    arrived_at DATETIME,
    
    FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE,
    FOREIGN KEY (product_id) REFERENCES products(product_id) ON DELETE CASCADE,

    -- インデックスをテーブル定義内に記述
    INDEX idx_orders_user_id_created_at (user_id, created_at DESC),
    -- INDEX idx_orders_shipped_status (shipped_status), -- [被替换] 这个索引效率较低
    INDEX idx_orders_arrived_at (arrived_at),

    -- --- 优化修改 START ---

    -- 1. 优化机器人配货 (robot.go -> GetShippingOrders)
    --    该查询使用 shipped_status 过滤, 并 JOIN product_id
    --    复合索引 (shipped_status, product_id) 可以最高效地支持此查询
    INDEX idx_orders_status_product (shipped_status, product_id),
    
    -- 2. 优化订单列表 (order.go -> ListOrders)
    --    该查询需要 JOIN product_id. 为 product_id 添加索引可以加速 JOIN
    INDEX idx_orders_product_id (product_id)
    
    -- --- 优化修改 END ---
);

CREATE TABLE `user_sessions` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `session_uuid` VARCHAR(36) NOT NULL,
  `user_id` INT UNSIGNED NOT NULL,
  `expires_at` DATETIME NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `session_uuid` (`session_uuid`),
  FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE
);

