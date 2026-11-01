cmake_minimum_required(VERSION 3.20)
project(yousdb_cpp LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

set(YOUSDB_BUILD_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../../build" CACHE PATH "Directory containing the YousDB C ABI library")
if(APPLE)
    set(YOUSDB_LIBRARY "${YOUSDB_BUILD_DIR}/libyousdb.dylib")
elseif(WIN32)
    set(YOUSDB_LIBRARY "${YOUSDB_BUILD_DIR}/yousdb.dll")
else()
    set(YOUSDB_LIBRARY "${YOUSDB_BUILD_DIR}/libyousdb.so")
endif()

if(NOT EXISTS "${YOUSDB_BUILD_DIR}/libyousdb.h")
    message(FATAL_ERROR "${YOUSDB_BUILD_DIR}/libyousdb.h is missing; run `make shared` first")
endif()

add_library(yousdb_cpp STATIC src/yousdb.cpp)
target_include_directories(yousdb_cpp PUBLIC include "${YOUSDB_BUILD_DIR}")
target_link_libraries(yousdb_cpp PUBLIC "${YOUSDB_LIBRARY}")
if(APPLE)
    set_target_properties(yousdb_cpp PROPERTIES INSTALL_RPATH "@loader_path;${YOUSDB_BUILD_DIR}")
elseif(UNIX)
    set_target_properties(yousdb_cpp PROPERTIES INSTALL_RPATH "${YOUSDB_BUILD_DIR}")
endif()

add_executable(yousdb-cpp src/main.cpp)
target_link_libraries(yousdb-cpp PRIVATE yousdb_cpp)

add_executable(yousdb-cpp-test test/integration.cpp)
target_link_libraries(yousdb-cpp-test PRIVATE yousdb_cpp)

enable_testing()
add_test(NAME cpp_integration COMMAND yousdb-cpp-test "${CMAKE_CURRENT_BINARY_DIR}/integration.db")
