// Quick and dirty simdjson benchmark: parse kafka.json in a for loop, time it.
#include <chrono>
#include <iostream>
#include "simdjson.h"
using namespace simdjson;

int main() {
    padded_string json = padded_string::load("kafka.json");
    ondemand::parser parser;

    const int RUNS = 100000;

    // warmup
    for (int i = 0; i < 1000; i++) {
        ondemand::document doc = parser.iterate(json);
        for (auto field : doc.get_object()) (void)field;
    }

    auto start = std::chrono::steady_clock::now();
    for (int i = 0; i < RUNS; i++) {
        ondemand::document doc = parser.iterate(json);
        for (auto field : doc.get_object()) (void)field;
    }
    auto end = std::chrono::steady_clock::now();

    double secs = std::chrono::duration<double>(end - start).count();
    double per_parse_ns = secs * 1e9 / RUNS;
    double mb_per_sec = (json.size() * (double)RUNS / (1024.0 * 1024.0)) / secs;
    double parses_per_sec = RUNS / secs;

    std::cout << "file: kafka.json (" << json.size() << " bytes)\n";
    std::cout << "runs: " << RUNS << "\n";
    std::cout << "total: " << secs << " s\n";
    std::cout << "per parse: " << per_parse_ns << " ns\n";
    std::cout << "throughput: " << mb_per_sec << " MB/s\n";
    std::cout << "parses/sec: " << parses_per_sec << "\n";
    return 0;
}
