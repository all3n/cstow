cmake_minimum_required(VERSION 3.16)
project(use-mylib LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
add_executable(use-mylib ${SOURCES})
target_include_directories(${PROJECT_NAME} PUBLIC include)

# Find mylib from cstow_deps (local dependency symlink)
find_path(MYLIB_INCLUDE_DIR NAMES mylib/math.hpp)
find_library(MYLIB_LIBRARY NAMES mylib)

if(MYLIB_INCLUDE_DIR AND MYLIB_LIBRARY)
    target_include_directories(use-mylib PRIVATE ${MYLIB_INCLUDE_DIR})
    target_link_libraries(use-mylib PRIVATE ${MYLIB_LIBRARY})
else()
    # Fallback: use relative include from cstow_deps
    target_include_directories(use-mylib PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/mylib/include)
    target_link_libraries(use-mylib PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/mylib/lib/libmylib.a)
endif()
