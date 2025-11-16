-- このファイルに記述されたSQLコマンドが、マイグレーション時に実行されます。
-- 在 0_sample.sql 的最后加上（或在插入样例订单之后加）
UPDATE orders o
JOIN products p ON o.product_id = p.product_id
SET
    o.value  = p.value,
    o.weight = p.weight
WHERE
    (o.value = 0 OR o.weight = 0);

