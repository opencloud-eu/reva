## Acceptance Tests (based on OpenCloud acceptance tests)

This will require some PHP-related tools to run, for instance on Ubuntu you will need `apt install -y php-xml php-curl composer`.

1. [Build reva](https://github.com/cs3org/reva/tree/edge?tab=readme-ov-file#build)

2. Start the reva server (with decomposed storage driver)

   ```bash
   make reva
   ```

   > **INFO:**
   >
   > You can run reva with other storage drivers.
   >
   > To run reva with `posix` storage driver:
   >
   > ```bash
   > make reva-posix
   > ```
   >
   > To run reva with `decomposeds3` storage driver:
   >
   > ```bash
   > make reva-decomposeds3
   > ```

3. Get OpenCloud

   ```bash
   git clone https://github.com/opencloud-eu/opencloud.git ./testrunner
   ```

4. To run the correct version of the testsuite check out the commit id from the `.drone.env` file

   ```bash
   cd testrunner
   git checkout <commit-id>
   ```

5. Run the tests

   ```bash
   TEST_SERVER_URL='http://localhost:20080' \
   TEST_REVA='true' \
   TEST_WITH_LDAP='true' \
   OC_REVA_DATA_ROOT='/tmp/reva/' \
   DELETE_USER_DATA_CMD="rm -rf /tmp/reva/data/nodes/root/* /tmp/reva/data/nodes/*-*-*-* /tmp/reva/data/blobs/*" \
   REVA_LDAP_HOSTNAME='localhost' \
   BEHAT_FILTER_TAGS='~@skip&&~@skipOnReva&&~@env-config' \
   EXPECTED_FAILURES_FILE=<path-to-reva>/tests/acceptance/expected-failures-on-decomposed-storage.md \
   DIVIDE_INTO_NUM_PARTS=1 \
   RUN_PART=1 \
   ACCEPTANCE_TEST_TYPE=core-api \
   make test-acceptance-api
   ```

   This will run all tests that are relevant to reva.

   To run a single test add `BEHAT_FEATURE=<feature file>` and specify the path to the feature file and an optional line number.
   For example:

   ```bash
   ...
   BEHAT_FEATURE='tests/acceptance/features/coreApiWebdavUpload/uploadFile.feature:20' \
   make test-acceptance-api
   ```

   **NOTE:**
   Make sure to double check the paths if you are changing the `OC_REVA_DATA_ROOT`. The `DELETE_USER_DATA_CMD` needs to clean up the correct folders.

   > **INFO:**
   >
   > Use these proper environment variables to run tests with different storage drivers:
   >
   > 1. Run tests with `decomposeds3` storage driver:
   >
   >    ```bash
   >    ...
   >    DELETE_USER_DATA_CMD="rm -rf /tmp/reva/data/spaces/* /tmp/reva/data/blobs/* /tmp/reva/data/indexes/by-type/*" \
   >    EXPECTED_FAILURES_FILE=<path-to-reva>/tests/acceptance/expected-failures-on-DECOMPOSEDS3-storage.md \
   >    make test-acceptance-api
   >    ```
   >
   > 2. Run tests with `posix` storage driver:
   >
   >    ```bash
   >    ...
   >    DELETE_USER_DATA_CMD="bash -cx 'rm -rf /tmp/reva/data/users/* /tmp/reva/data/indexes/by-type/*'" \
   >    EXPECTED_FAILURES_FILE=<path-to-reva>/tests/acceptance/expected-failures-on-POSIX-storage.md \
   >    make test-acceptance-api
   >    ```

6. Cleanup the setup

   ```bash
   make clean
   ```
