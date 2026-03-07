cmake_minimum_required(VERSION 3.16)
project(libdave_c C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

# Find libdave via vcpkg or system install
find_package(dave QUIET)

if(NOT dave_FOUND)
    # Fallback: build libdave from source via FetchContent
    include(FetchContent)

    FetchContent_Declare(
        libdave
        GIT_REPOSITORY https://github.com/discord/libdave.git
        GIT_TAG        main
    )
    FetchContent_MakeAvailable(libdave)
endif()

add_library(dave_c STATIC libdave_c.cpp)

target_include_directories(dave_c PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}
)

target_link_libraries(dave_c PUBLIC
    dave::dave
)

# Install target for convenience
install(TARGETS dave_c
    ARCHIVE DESTINATION lib
)
install(FILES libdave_c.h DESTINATION include)
