cmake_minimum_required(VERSION 3.20)
project(soldierdb_cpp LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

set(SOLDIERDB_BUILD_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../../build" CACHE PATH "Directory containing the SoldierDB C ABI library")
if(APPLE)
    set(SOLDIERDB_LIBRARY "${SOLDIERDB_BUILD_DIR}/libsoldierdb.dylib")
elseif(WIN32)
    set(SOLDIERDB_LIBRARY "${SOLDIERDB_BUILD_DIR}/soldierdb.dll")
else()
    set(SOLDIERDB_LIBRARY "${SOLDIERDB_BUILD_DIR}/libsoldierdb.so")
endif()

if(NOT EXISTS "${SOLDIERDB_BUILD_DIR}/libsoldierdb.h")
    message(FATAL_ERROR "${SOLDIERDB_BUILD_DIR}/libsoldierdb.h is missing; run `make shared` first")
endif()

add_library(soldierdb_cpp STATIC src/soldierdb.cpp)
target_include_directories(soldierdb_cpp PUBLIC include "${SOLDIERDB_BUILD_DIR}")
target_link_libraries(soldierdb_cpp PUBLIC "${SOLDIERDB_LIBRARY}")
if(APPLE)
    set_target_properties(soldierdb_cpp PROPERTIES INSTALL_RPATH "@loader_path;${SOLDIERDB_BUILD_DIR}")
elseif(UNIX)
    set_target_properties(soldierdb_cpp PROPERTIES INSTALL_RPATH "${SOLDIERDB_BUILD_DIR}")
endif()

add_executable(soldierdb-cpp src/main.cpp)
target_link_libraries(soldierdb-cpp PRIVATE soldierdb_cpp)

add_executable(soldierdb-cpp-test test/integration.cpp)
target_link_libraries(soldierdb-cpp-test PRIVATE soldierdb_cpp)

enable_testing()
add_test(NAME cpp_integration COMMAND soldierdb-cpp-test "${CMAKE_CURRENT_BINARY_DIR}/integration.db")
