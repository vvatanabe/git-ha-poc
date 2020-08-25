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
        name            VARCHAR(255) NOT NULL,
        cluster_name    VARCHAR(255) NOT NULL,
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
