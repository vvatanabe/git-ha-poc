CREATE TABLE IF NOT EXISTS zone (
        name    VARCHAR(255) NOT NULL,
        UNIQUE(name)
);
INSERT INTO zone VALUES (1, 'midgar');
INSERT INTO zone VALUES (2, 'goldsaucer');

CREATE TABLE IF NOT EXISTS user (
        name       VARCHAR(255) NOT NULL,
        zone_name  VARCHAR(255) NOT NULL,
        PRIMARY KEY (id),
        UNIQUE(name),
        CONSTRAINT fk_user_zone_name FOREIGN KEY (zone_name) REFERENCES zone (name)
);
INSERT INTO user VALUES (1, 'foo', 'midgar');
INSERT INTO user VALUES (2, 'bar', 'midgar');
INSERT INTO user VALUES (3, 'baz', 'goldsaucer');

CREATE TABLE IF NOT EXISTS repo (
        name       VARCHAR(255) NOT NULL,
        user_name    VARCHAR(255) NOT NULL,
        UNIQUE(name),
        CONSTRAINT fk_repo_user_name FOREIGN KEY (user_name) REFERENCES user (name)
);
