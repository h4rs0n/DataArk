-- +goose Up
-- loadDedupeArticle 与恢复查询按 m.current_version_id=r.version_id 连接 material；
-- 无索引时每篇文章重读都全表扫描 material。IF NOT EXISTS 允许先在运行库上
-- 以 CONCURRENTLY 方式建好同名索引，本迁移随后仅登记版本。
CREATE INDEX IF NOT EXISTS idx_material_current_version ON material(current_version_id);
-- +goose Down
DROP INDEX IF EXISTS idx_material_current_version;
