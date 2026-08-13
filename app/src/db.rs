use crate::TimeFilter;
use anyhow::{anyhow, Result};
use parking_lot::Mutex;
use rusqlite::Connection;
use std::collections::HashMap;
use std::sync::Arc;

#[derive(Clone, Debug)]
pub enum ColumnValue {
    String(String),
    Float(f64),
    Integer(i64),
}

#[derive(Clone, Debug)]
pub struct QueryResult {
    pub columns: HashMap<String, ColumnValue>,
}

pub const DURATION_THRESHOLD: i32 = 120;
/// Parse time string from SQLite time() function (HH:MM:SS) to hours
fn parse_time_string(s: &str) -> Option<f64> {
    let parts: Vec<&str> = s.split(':').collect();
    if parts.len() != 3 {
        return None;
    }

    let hours: f64 = parts[0].parse().ok()?;
    let minutes: f64 = parts[1].parse().ok()?;
    let seconds: f64 = parts[2].parse().ok()?;

    Some(hours + minutes / 60.0 + seconds / 3600.0)
}

#[derive(Clone)]
pub struct Database {
    conn: Arc<Mutex<Connection>>,
}

impl Database {
    pub fn new(path: &str) -> Result<Self> {
        let conn = Connection::open(path).map_err(|e| anyhow!("Failed to open database: {}", e))?;

        Ok(Database {
            conn: Arc::new(Mutex::new(conn)),
        })
    }

    /*
    /// Initialize the database schema
    pub fn init_schema(&self) -> Result<()> {
        let conn = self.conn.lock();
        conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS trackz (
                id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
                process_name VARCHAR(256) NOT NULL,
                app_name VARCHAR(256) NOT NULL,
                window_name VARCHAR(256) NOT NULL,
                focus_start_time INTEGER NOT NULL,
                focus_end_time INTEGER,
                process_owner VARCHAR(128)
            )",
        )
        .map_err(|e| anyhow!("Failed to initialize schema: {}", e))?;

        Ok(())
    } */

    pub fn get_top_apps_by_time(&self, limit: i32, filter: TimeFilter) -> Result<Vec<QueryResult>> {
        let conn = self.conn.lock();
        let start = filter.start_time.timestamp();
        let end = filter.end_time.timestamp();

        let sql = format!(
            "SELECT app_name, SUM(focus_end_time - focus_start_time) as total_time
         FROM trackz
         WHERE (focus_end_time - focus_start_time) > {}
           AND focus_start_time >= ?1
           AND focus_start_time <= ?2
         GROUP BY app_name
         ORDER BY total_time DESC
         LIMIT ?3",
            DURATION_THRESHOLD
        );

        let mut stmt = conn.prepare(&sql)?;

        let results = stmt.query_map([start, end, limit as i64], |row| {
            let mut result = QueryResult::new();
            result.insert("app_name", ColumnValue::String(row.get(0)?));
            result.insert("total_time", ColumnValue::Integer(row.get(1)?));
            Ok(result)
        })?;

        let mut data = Vec::new();
        for result in results {
            data.push(result?);
        }
        Ok(data)
    }

    pub fn get_top_apps_pie_chart(
        &self,
        limit: i32,
        filter: TimeFilter,
    ) -> Result<(Vec<String>, Vec<QueryResult>)> {
        let conn = self.conn.lock();

        let start = filter.start_time.timestamp();
        let end = filter.end_time.timestamp();

        let sql = format!(
        "SELECT process_name, window_name,
                sum(CASE WHEN focus_end_time IS NOT NULL THEN focus_end_time - focus_start_time ELSE 0 END) as duration
         FROM trackz
         WHERE (focus_end_time - focus_start_time) > {}
           AND focus_start_time >= ?1
           AND focus_start_time <= ?2
         GROUP BY process_name, window_name
         ORDER BY duration DESC
         LIMIT ?3",
        DURATION_THRESHOLD
    );

        let mut stmt = conn.prepare(&sql)?;
        let column_names: Vec<String> = stmt.column_names().iter().map(|s| s.to_string()).collect();

        let results = stmt.query_map([start, end, limit as i64], |row| {
            let process_name: String = row.get(0)?;
            let window_name: String = row.get(1)?;
            let duration_seconds: i64 = row.get(2)?;
            let duration_hours = duration_seconds as f64 / 3600.0;

            let mut result = QueryResult::new();
            result.insert("process_name", ColumnValue::String(process_name.clone()));
            result.insert("window_name", ColumnValue::String(window_name.clone()));
            result.insert(
                "label",
                //ColumnValue::String(format!("{}: {}", process_name, window_name)),
                ColumnValue::String(format!("{}", window_name)),
            );
            result.insert("duration", ColumnValue::Float(duration_hours));

            Ok(result)
        })?;

        let mut data = Vec::new();
        for result in results {
            data.push(result?);
        }

        Ok((column_names, data))
    }

    pub fn get_time_spent_per_app(&self, filter: TimeFilter) -> Result<Vec<QueryResult>> {
        let conn = self.conn.lock();
        let start = filter.start_time.timestamp();
        let end = filter.end_time.timestamp();

        let sql = format!(
            "SELECT app_name, SUM(focus_end_time - focus_start_time) as total_time
         FROM trackz
         WHERE (focus_end_time - focus_start_time) > {}
           AND focus_start_time >= ?1
           AND focus_start_time <= ?2
         GROUP BY app_name
         ORDER BY total_time DESC",
            DURATION_THRESHOLD
        );

        let mut stmt = conn.prepare(&sql)?;

        let results = stmt.query_map([start, end], |row| {
            let total_seconds: i64 = row.get(1)?;
            let total_hours = total_seconds as f64 / 3600.0;

            let mut result = QueryResult::new();
            result.insert("app_name", ColumnValue::String(row.get(0)?));
            result.insert("total_time", ColumnValue::Float(total_hours));
            Ok(result)
        })?;

        let mut data = Vec::new();
        for result in results {
            data.push(result?);
        }
        Ok(data)
    }

    pub fn get_window_activity_timeline(&self, filter: TimeFilter) -> Result<Vec<QueryResult>> {
        let conn = self.conn.lock();
        let start = filter.start_time.timestamp();
        let end = filter.end_time.timestamp();

        let mut stmt = conn.prepare(
            "SELECT app_name, focus_start_time, COUNT(*) as count
         FROM trackz
         WHERE focus_start_time >= ?1
           AND focus_start_time <= ?2
         GROUP BY app_name, focus_start_time
         ORDER BY focus_start_time",
        )?;

        let results = stmt.query_map([start, end], |row| {
            let mut result = QueryResult::new();
            result.insert("app_name", ColumnValue::String(row.get(0)?));
            result.insert("focus_start_time", ColumnValue::Integer(row.get(1)?));
            result.insert("count", ColumnValue::Integer(row.get(2)?));
            Ok(result)
        })?;

        let mut data = Vec::new();
        for result in results {
            data.push(result?);
        }
        Ok(data)
    }

    pub fn get_daily_stats(&self, filter: TimeFilter) -> Result<Vec<QueryResult>> {
        let conn = self.conn.lock();
        let start = filter.start_time.timestamp();
        let end = filter.end_time.timestamp();

        let sql = format!(
            "SELECT 
            DATE(datetime(focus_start_time, 'unixepoch')) as day,
            SUM(focus_end_time - focus_start_time) as total_time,
            COUNT(DISTINCT app_name) as app_count
         FROM trackz
         WHERE (focus_end_time - focus_start_time) > {}
           AND focus_start_time >= ?1
           AND focus_start_time <= ?2
         GROUP BY DATE(datetime(focus_start_time, 'unixepoch'))
         ORDER BY day",
            DURATION_THRESHOLD
        );

        let mut stmt = conn.prepare(&sql)?;

        let results = stmt.query_map([start, end], |row| {
            let total_seconds: i64 = row.get(1)?;
            let total_hours = total_seconds as f64 / 3600.0;

            let mut result = QueryResult::new();
            result.insert("day", ColumnValue::String(row.get(0)?));
            result.insert("total_time", ColumnValue::Float(total_hours));
            result.insert("app_count", ColumnValue::Integer(row.get(2)?));
            Ok(result)
        })?;

        let mut data = Vec::new();
        for result in results {
            data.push(result?);
        }
        Ok(data)
    }

    pub fn get_most_used_windows(
        &self,
        limit: i32,
        filter: TimeFilter,
    ) -> Result<Vec<QueryResult>> {
        let conn = self.conn.lock();
        let start = filter.start_time.timestamp();
        let end = filter.end_time.timestamp();

        let mut stmt = conn.prepare(
            "SELECT window_name, COUNT(*) as focus_count
         FROM trackz
         WHERE focus_start_time >= ?1
           AND focus_start_time <= ?2
         GROUP BY window_name
         ORDER BY focus_count DESC
         LIMIT ?3",
        )?;

        let results = stmt.query_map([start, end, limit as i64], |row| {
            let mut result = QueryResult::new();
            result.insert("window_name", ColumnValue::String(row.get(0)?));
            result.insert("focus_count", ColumnValue::Integer(row.get(1)?));
            Ok(result)
        })?;

        let mut data = Vec::new();
        for result in results {
            data.push(result?);
        }
        Ok(data)
    }

    pub fn execute_query(&self, sql: &str) -> Result<(Vec<String>, Vec<QueryResult>)> {
        if !sql.trim().to_uppercase().starts_with("SELECT") {
            return Err(anyhow!("Only SELECT queries are allowed"));
        }

        let conn = self.conn.lock();
        let mut stmt = conn.prepare(sql)?;

        let column_names: Vec<String> = stmt.column_names().iter().map(|s| s.to_string()).collect();

        if column_names.is_empty() {
            return Err(anyhow!("Query must return at least 1 column"));
        }

        let results = stmt.query_map([], |row| {
            let mut result = QueryResult::new();

            for (col_idx, col_name) in column_names.iter().enumerate() {
                let value = match row.get::<_, f64>(col_idx) {
                    Ok(v) => ColumnValue::Float(v),
                    Err(_) => match row.get::<_, i64>(col_idx) {
                        Ok(v) => ColumnValue::Integer(v),
                        Err(_) => match row.get::<_, String>(col_idx) {
                            Ok(s) => {
                                if let Some(parsed) = parse_time_string(&s) {
                                    ColumnValue::Float(parsed)
                                } else {
                                    ColumnValue::String(s)
                                }
                            }
                            Err(e) => return Err(e),
                        },
                    },
                };
                result.insert(col_name.clone(), value);
            }
            Ok(result)
        })?;

        let mut data = Vec::new();
        for result in results {
            data.push(result?);
        }

        Ok((column_names, data))
    }
    pub fn finalise_active_record(&self, end_time: i64) -> Result<()> {
        let conn = self.conn.lock();

        conn.execute(
            "UPDATE trackz
             SET focus_end_time = ?1
             WHERE focus_end_time IS NULL
             ORDER BY focus_start_time DESC
             LIMIT 1",
            [end_time],
        )?;

        Ok(())
    }

    /// Insert a new sleep record
    pub fn insert_sleep_record(&self, start_time: i64) -> Result<()> {
        let conn = self.conn.lock();

        conn.execute(
            "INSERT INTO trackz
             (process_name, app_name, window_name, focus_start_time, focus_end_time, process_owner)
             VALUES (?, ?, ?, ?, NULL, ?)",
            rusqlite::params!["sleep", "sleep_mode", "System Sleep", start_time, "system",],
        )?;

        Ok(())
    }

    pub fn finalise_sleep_record(&self, end_time: i64) -> Result<()> {
        let conn = self.conn.lock();

        let sql = "UPDATE trackz
               SET focus_end_time = ?1
               WHERE rowid = (
                   SELECT rowid FROM trackz
                   WHERE app_name = 'sleep_mode'
                     AND focus_end_time IS NULL
                   ORDER BY focus_start_time DESC
                   LIMIT 1
               )";

        match conn.execute(sql, [end_time]) {
            Ok(rows_affected) if rows_affected == 0 => {
                Err(anyhow!("No active sleep record found to finalise"))
            }
            Ok(_) => Ok(()),
            Err(e) => Err(anyhow!("Failed to finalise sleep record: {}", e)),
        }
    }
}
impl QueryResult {
    pub fn new() -> Self {
        Self {
            columns: HashMap::new(),
        }
    }

    pub fn insert(&mut self, name: impl Into<String>, value: ColumnValue) {
        self.columns.insert(name.into(), value);
    }

    pub fn get_string(&self, column: &str) -> Option<String> {
        match self.columns.get(column) {
            Some(ColumnValue::String(s)) => Some(s.clone()),
            _ => None,
        }
    }

    pub fn get_float(&self, column: &str) -> Option<f64> {
        match self.columns.get(column) {
            Some(ColumnValue::Float(f)) => Some(*f),
            Some(ColumnValue::Integer(i)) => Some(*i as f64),
            _ => None,
        }
    }

    pub fn get_integer(&self, column: &str) -> Option<i64> {
        match self.columns.get(column) {
            Some(ColumnValue::Integer(i)) => Some(*i),
            Some(ColumnValue::Float(f)) => Some(*f as i64),
            _ => None,
        }
    }
}
