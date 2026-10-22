<?php

declare(strict_types=1);

header('Content-Type: text/plain; version=0.0.4; charset=utf-8');
header('Cache-Control: no-store');

if (($_SERVER['REQUEST_METHOD'] ?? '') !== 'GET') {
    http_response_code(405);
    exit;
}

$databaseAddress = getenv('WORDPRESS_DB_HOST') ?: 'database:3306';
$addressParts = explode(':', $databaseAddress, 2);
$databaseHost = $addressParts[0];
$databasePort = isset($addressParts[1]) ? (int)$addressParts[1] : 3306;

$database = @mysqli_init();
if ($database === false || !$database->options(MYSQLI_OPT_CONNECT_TIMEOUT, 2)) {
    http_response_code(503);
    exit;
}

$connected = @$database->real_connect(
    $databaseHost,
    getenv('WORDPRESS_DB_USER') ?: '',
    getenv('WORDPRESS_DB_PASSWORD') ?: '',
    getenv('WORDPRESS_DB_NAME') ?: '',
    $databasePort
);
if (!$connected) {
    http_response_code(503);
    exit;
}

$metrics = [
    'Threads_connected' => ['damocline_mariadb_threads_connected', 'gauge', 'Current client connections.'],
    'Threads_running' => ['damocline_mariadb_threads_running', 'gauge', 'Currently executing threads.'],
    'Queries' => ['damocline_mariadb_queries_total', 'counter', 'Statements executed since server start.'],
    'Slow_queries' => ['damocline_mariadb_slow_queries_total', 'counter', 'Slow statements since server start.'],
    'Connections' => ['damocline_mariadb_connections_total', 'counter', 'Connection attempts since server start.'],
    'Innodb_buffer_pool_reads' => ['damocline_mariadb_innodb_buffer_pool_reads_total', 'counter', 'InnoDB reads that required a disk access.'],
    'Innodb_buffer_pool_read_requests' => ['damocline_mariadb_innodb_buffer_pool_read_requests_total', 'counter', 'Logical InnoDB buffer-pool read requests.'],
    'Table_locks_waited' => ['damocline_mariadb_table_locks_waited_total', 'counter', 'Table lock requests that could not be granted immediately.'],
    'Created_tmp_disk_tables' => ['damocline_mariadb_created_tmp_disk_tables_total', 'counter', 'Internal temporary tables created on disk.'],
    'Handler_read_rnd_next' => ['damocline_mariadb_handler_read_rnd_next_total', 'counter', 'Sequential row reads requested by storage handlers.'],
];

echo "# HELP damocline_mariadb_up Whether the read-only MariaDB status query succeeded.\n";
echo "# TYPE damocline_mariadb_up gauge\n";
echo "damocline_mariadb_up 1\n";

$quotedNames = "'" . implode("','", array_keys($metrics)) . "'";
$result = @$database->query("SHOW GLOBAL STATUS WHERE Variable_name IN ($quotedNames)");
if ($result === false) {
    http_response_code(503);
    exit;
}

$values = [];
while ($row = $result->fetch_row()) {
    if (isset($metrics[$row[0]]) && is_numeric($row[1])) {
        $values[$row[0]] = $row[1];
    }
}
$result->free();
$database->close();

if (count($values) !== count($metrics)) {
    http_response_code(503);
    exit;
}

foreach ($metrics as $statusName => [$metricName, $type, $description]) {
    if (!isset($values[$statusName])) {
        continue;
    }
    echo "# HELP $metricName $description\n";
    echo "# TYPE $metricName $type\n";
    echo "$metricName $values[$statusName]\n";
}
