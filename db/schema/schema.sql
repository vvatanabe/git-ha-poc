CREATE TABLE IF NOT EXISTS zone (
        name    VARCHAR(255) NOT NULL,
        UNIQUE(name)
);
INSERT INTO zone VALUES ('midgar');
INSERT INTO zone VALUES ('goldsaucer');

CREATE TABLE IF NOT EXISTS user (
        name       VARCHAR(255) NOT NULL,
        zone_name  VARCHAR(255) NOT NULL,
        UNIQUE(name),
        CONSTRAINT fk_user_zone_name FOREIGN KEY (zone_name) REFERENCES zone (name)
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
