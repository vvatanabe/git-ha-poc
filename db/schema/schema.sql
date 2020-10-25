CREATE TABLE IF NOT EXISTS cluster (
        name    VARCHAR(255) NOT NULL,
        UNIQUE(name)
);
INSERT INTO cluster VALUES ('midgar');
INSERT INTO cluster VALUES ('goldsaucer');

CREATE TABLE IF NOT EXISTS node (
        name            VARCHAR(255) NOT NULL,
        cluster_name    VARCHAR(255) NOT NULL,
        addr            VARCHAR(255) NOT NULL,
        writable        tinyint(1) NOT NULL DEFAULT 0,
        active          tinyint(1) NOT NULL DEFAULT 0,
        UNIQUE(name),
        CONSTRAINT fk_node_cluster_name FOREIGN KEY (cluster_name) REFERENCES cluster (name)
);
INSERT INTO node VALUES ('tadpole-1', 'midgar', 'tadpole-1-a:50051', 1, 1);
INSERT INTO node VALUES ('tadpole-2', 'midgar', 'tadpole-2-a:50051', 0, 1);
INSERT INTO node VALUES ('tadpole-3', 'midgar', 'tadpole-3-b:50051', 0, 1);
INSERT INTO node VALUES ('tadpole-4', 'goldsaucer', 'tadpole-4-b:50051', 1, 1);
INSERT INTO node VALUES ('tadpole-5', 'goldsaucer', 'tadpole-5-b:50051', 0, 1);
INSERT INTO node VALUES ('tadpole-6', 'goldsaucer', 'tadpole-6-a:50051', 0, 1);

CREATE TABLE IF NOT EXISTS user (
        name                  VARCHAR(255) NOT NULL,
        encrypted_password    BINARY(60) NOT NULL,
        ssh_key               TEXT NOT NULL,
        cluster_name          VARCHAR(255) NOT NULL,
        UNIQUE(name),
        CONSTRAINT fk_user_cluster_name FOREIGN KEY (cluster_name) REFERENCES cluster (name)
);
INSERT INTO user VALUES ('foo', 'midgar');
INSERT INTO user VALUES ('bar', 'midgar');
INSERT INTO user VALUES ('baz', 'goldsaucer');

CREATE TABLE IF NOT EXISTS repo (
        name       VARCHAR(255) NOT NULL,
        user_name    VARCHAR(255) NOT NULL,
        UNIQUE(name),
        CONSTRAINT fk_repo_user_name FOREIGN KEY (user_name) REFERENCES user (name)
);

CREATE TABLE IF NOT EXISTS jwdk_queue_attributes (
        name                     VARCHAR(255) NOT NULL,
        raw_name                 VARCHAR(255) NOT NULL,
        visibility_timeout       INTEGER UNSIGNED NOT NULL,
        delay_seconds            INTEGER UNSIGNED NOT NULL,
        max_receive_count        INTEGER UNSIGNED NOT NULL,
        dead_letter_target       VARCHAR(255),
        UNIQUE(name),
        UNIQUE(raw_name)
);

INSERT INTO jwdk_queue_attributes (name, raw_name, visibility_timeout, delay_seconds, dead_letter_target, max_receive_count)
VALUES ("replication_queue", "jwdk_replication_queue", 60, 2, "", 10);

CREATE TABLE IF NOT EXISTS jwdk_replication_queue (
        sec_id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        job_id            VARCHAR(255) NOT NULL,
        content           TEXT,
        deduplication_id  VARCHAR(255),
        group_id          VARCHAR(255),
        invisible_until   BIGINT UNSIGNED NOT NULL,
        retry_count       INTEGER UNSIGNED NOT NULL,
        enqueue_at        BIGINT UNSIGNED,
        PRIMARY KEY (sec_id),
        UNIQUE(deduplication_id),
        INDEX jwdk_replication_queue_idx_invisible_until_retry_count (invisible_until, retry_count)
);