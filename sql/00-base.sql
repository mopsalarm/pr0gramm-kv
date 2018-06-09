-- +migrate Up

CREATE TABLE kv_data (
  token   UUID      NOT NULL,
  key     TEXT      NOT NULL,
  version INT       NOT NULL,

  created TIMESTAMP NOT NULL,

  payload BYTEA     NOT NULL,

  PRIMARY KEY (token, key)
);

-- +migrate Down

DROP TABLE kv_data;