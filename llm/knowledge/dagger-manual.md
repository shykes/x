Dagger manual

This manual explains how to use dagger.

    ## dagger is powerful and different

    Dagger is a powerful tool for orchestrating containers and building any sort of artifact in repeatable ephemeral environments.
    Dagger has a simple HTTP API, with clients in several programming languages, and a bash-like scripting language.

    ## dagger vs. bash

    Dagger's scripting syntax is superficially similar to bash, but works very differently under the hood.
    Think "bash meets powershell meets Dockerfile meets Docker Compose"

    ## Multi-line scripts

    Dagger does not support multi-line scripts. The only exception is multi-line quoted strings, for example to pass a multi-line string as argument.

    <example>
      <description>INCORRECT multi-line script</description>
      <script>
      container |
      # Download from alpine
      from alpine |
      with-exec \
      -- uname -a |
      stdout
      </script>
    </example>


    <example>
      <description>CORRECT single-line script with a multi-line quoted string argument
      <script>
      container | from alpine | with-new-file hello.txt "hello
      how

      are

      you?" | with-exec -- uname -a | stdout
      </script>
    </example>

    ## immutable artifacts

    All state in Dagger is an immutable artifact. Whether a directory, file, container or scalar value: all operations take immutable   snapshots as input, and produce more immutable snapshots.

    Once you are familiar with it, this model is very powerful because it gives you repeatability by default.

    ## Sandbox

    Dagger provides you with an ephemeral sandbox that is initially empty, but can be filled with any number of containers and artifacts. In this way, you are not limited to the constraints of what your host computer can store or execute: dagger is your canvas   for building and running any software you want - from only the building blocks of git, http, containers, and basic filesystem   operations. Think carefully about this paradigm, as it is the key to dagger's power.


    ## Host access

    Although the Dagger sandbox transcends your host computer, it still allows limited access to it, to read and write files or access  network services for example. See "how to read and write files on the computer".

    ## functions

    Functions are the fundamental building block for interacting with the sandbox. Functions are a repeatable task that can be invoked  by name, can receive arguments, and can return a value or an error.

    The syntax for executing a function is the same as running a command in bash. Mandatory arguments are positional; optional arguments are flags. The safest syntax for passing arguments is '--foo=bar'. For booleans you can use '--foo=true' or  '--foo=false'. As a convenience '--foo' means '--foo=true'.

    <example>
        <description>
            execute the function 'foo' with the optional boolean argument 'debug', the optional string argument'name', and one mandatory argument
        </description>
        <script>
            foo --debug --name=bob bar
        </script>
    </example>

    Unlike regular shell commands, dagger functions are tightly sandboxed (they have no access to your host computer). They canonly    access     resources you give them as arguments. They have no implicit side effect - their only output is their returnvalue or     error.

    Functions can receive and return scalar values or complex artifacts like files or directories - but always as immutablesnapshots.

    In order to use the result of a function, you chain them to other functions with *pipelines*.

    ## Objects

        Objects are complex data structures that can be passed to a function as argument, and be returned by a function.

        The Dagger type system defines certain object types for Directories, Files, Containers and other artifacts.
        Dagger can also be extended with custom object types.

        Objects contain 1) immutable state, and 2) functions which can be called, like methods.

    ## Pipelines

        When a function returns an object, a function in that object can be called, and so on, to form a chain of functions called a    "pipeline". The    state of each object is available to the next function. This makes dagger pipelines for builds, data pipelines,  and other "factory-like" tasks  that involve one-way flow of artifacts.

        <example>
            <description>
                pipeline of four functions: create an empty directory; add a new directory within it; add a new file; then list all files
            </description>
            <script>
                directory | with-directory foo/bar | with-new-file /foo/bar/hello.txt 'Hi there!' | glob '**'
            </script>
        </example>

        The result of a pipeline is the return value of the last function in the pipeline, or the first error encountered.

    ## Container orchestration

        Dagger can orchestrate containers natively. This includes building and running - in Dagger there is no hard-cut difference.

        The only way to execute a tool in Dagger is to build or download a container *then* to execute the tool in that container. Again,   this offers   strong repeatability guarantees.

        <example>
            <description>
                orchestrate building a software package from source in a container, and export it to the host
            </description>
            <script>
                container | from alpine:latest | with-exec apk add curl openssh git go | with-workdir /src | with-exec git clone https://github.com/dagger/dagger | with-workdir ./dagger | with-exec go build ./cmd/dagger | file ./dagger | export ./build/dagger
            </script>
        </example>

    ## Sub-pipelines

        Dagger can use the result of a pipeline as the argument to a function, as long as the types match.

        This is very useful for assembling complex pipelines out of simple building blocks.

        <example>
            <description>
                multi-stage build
            </description>
            <script>
                container | from ubuntu | with-file /bin/dagger $(container | from alpine | with-exec apk add go | with-workdir /src | with-directory . $(git https://github.com/dagger/dagger | head | tree) | with-exec go build ./cmd/dagger) | with-exec dagger version | stdout
            </script>
        </example>

    ## Types

        All dagger functions are strongly typed. All types can be introspected by calling the special builtin function .doc in any  context. It will  print the type of the current scope, and available functions.

        <example>
            <description>
                show available functions in the current scope
            </description>
            <script>
                .doc
            </script>
        </example>

        <example>
            <description>
                start a pipeline, then show what functions are available to run next in the pipeline
            </description>
            <script>
                container | .doc
            </script>
        </example>

        <example>
            <description>
                show available functions for directory type
            </description>
            <script>
                directory | .doc
            </script>
        </example>

        <example>
            <description>
                get detailed documnetation on a particular function in the current scope
            </description>
            <script>
                .doc git
            </script>
        </example>

        <example>
            <description>
                call a function, then get detailed documentation on a particular function in the resulting object
            </description>
            <script>
                directory | .doc with-new-file
            </script>
        </example>

    ## Filesystem operations

        Filesystem operations work from immutable snapshots of files and directories.

        <example>
            <description>
                create a new file and add a directory from git
            </description>
            <script>
                directory | with-new-file hello.txt 'Hello, world' | with-directory ./src $(git https://github.com/goreleaser/goreleaser | head | tree)
            </script>
        </example>

    ## Newlines and tabs

    The dagger scripting language does not interpret special characters like "\t" or "\n". Instead use quotes and literal newlines and tabs.

    <example>
      <description>
        Create a file containing newlines and tabs
      </description>
      <script>
        directory | with-new-file hello.txt "This is the title

        This is the first paragraph.

        This is the second paragraph, and it  has tabs.
      </script>
    </example>

    ## Builtins

    Dagger exposes special functions called builtins. Their name starts with '.'

    <example>
        <description>
            list available builtins
        </description>
        <script>
            .help
        </script>
    </example>

    The most commonly used builtin is .doc


    ## Discovering commands

        To discover available commands, use type introspection via the .doc builtin abundantly.

        <example>
            <description>
                explore available commands
            </description>
            <script>
                .help
                .doc
                .doc container
                container | .doc
                container | from alpine
            </script>
        </example>

        <example>
            <description>
                build Go project from git
            </description>
            <script>
                container | from index.docker.io/golang | with-directory /src $(.git https://github.com/goreleaser/goreleaser | head | tree) | with-workdir /src | with-exec go build ./... | directory ./bin
            </script>
        </example>

        <example>
            <description>
                create file with contents from git
            </description>
            <script>
                directory | with-new-file goreleaser-readme.md $(git https://github.com/goreleaser/goreleaser | tags | tree | file README.md | contents)
            </script>
        </example>

    ## Modules
        Dagger can be extended with additional types and functions, packaged in special directories called modules. Modules can be loaded   from any  directory, and published on any git server.

        To load a module, use its full address as a function name, and either 1) execute it, or 2) inspect it with .doc

        <example>
            <description>
                dagger session loading a module from the goreleaser repository, and exploring its contents (each line is a command)
            </description>
            <script>
                .doc github.com/goreleaser/goreleaser
                github.com/goreleaser/goreleaser | .doc
                github.com/goreleaser/goreleaser | .doc base
                github.com/goreleaser/goreleaser | base | .doc
                github.com/goreleaser/goreleaser | base | with-exec -- goreleaser --help | stdout
            </script>
        </example>

        <example>
            <description>
                exploring and using the module at github.com/dagger/dagger/modules/go
            </description>
            <script>
                .doc github.com/dagger/dagger/modules/go
                git https://github.com/golang/example | head | tree | entries
                git https://github.com/golang/example | head | tree | directory ./hello | file go.mod
                github.com/dagger/dagger/modules/go --version=1.23.0 $(git https://github.com/golang/example | head | tree | directory ./hello) | .doc
                container | from alpine | with-file /bin/hello $(github.com/dagger/dagger/modules/go --version=1.23.0 $ (githttps://github.com/golang/example | head | tree | directory ./hello) | build | file bin/hello) | with-exec hello | stdout
            </script>
        </example>

        <example>
            <description>
                inspect and use the module at github.com/shykes/x/termcast
            </description>
            <script>
                # Load module directly from address, inspect its contents, then build a pipeline
                github.com/shykes/x/termcast | .doc
                github.com/shykes/x/termcast | exec 'ls -l' | exec 'curl https://lemonde.fr' | gif
                git https://github.com/kpenfound/dagger-modules | head | tree | glob '**'
            </script>
        </example>

        <example>
            <description>
                exploring and using more modules
            </description>
            <script>
                .doc github.com/dagger/dagger/cmd/dagger
                github.com/dagger/dagger/cmd/dagger | .doc
                github.com/dagger/dagger/modules/wolfi | .doc
                github.com/dagger/dagger/modules/wolfi | container | with-file /bin/dagger $(github.com/dagger/dagger/cmd/dagger | binary) | with-exec dagger version | stdout
            </script>
        </example>

    ## Dockerfile equivalence

    Most dockerfile commands have an equivalent, but not always named the same. explore!

    <example>
        <description>
            'container | with-default-arg' is equivalent to CMD in Dockerfile
        </description>
        <script>
            container | .doc
            container | with-default-args bash -- -l
        </script>
    </example>

    ## Quoting
        Quoting works like in bash. Including the use of single and double quotes.

        <example>
            <description>
                use double quotes to allow variable expansion
            </description>
            <script>
                foo=bar; directory | with-new-file joke.txt "two programmers meet in a $foo"
            </script>
        </example>

        <example>
            <description>
                use single quote to avoid variable expansion
            </description>
            <script>
                directory | with-new-file script.sh 'echo "my user is $USER"'
            </script>
        </example>

    ## Double dash

        The double dash "--" has the same meaning as in unix shell. It's especially important when using with-exec,
        to make sure flags intended for the executed command, are not interpreted as arguments to with-exec.

        <example>
            <description>
                use double dash to avoid with-exec incorrectly interpreting arguments meant for the executed tool
            </description>
            <script>
                container | from alpine | with-exec -- ls -l
            </script>
        </example>

    <example>
        <description>
            various dagger commands
        </description>
        <script>
            .help
            .doc
            .doc container
            directory | .doc
            container | .doc
            container | from alpine | with-exec apk add openssh git | .doc publish
            container | from alpine | with-exec apk add openssh git | publish ttl.sh/my-image
            directory | with-new-file goreleaser-readme.md $(git https://github.com/goreleaser/goreleaser | head | tree | file README.md)
            directory | with-new-file goreleaser-readme.md $(git https://github.com/goreleaser/goreleaser | tags | tree | file README.md | contents )
            http https://news.ycombinator.com | contents
            directory | with-new-file hello.txt "hello world" | file hello.txt | .doc
            directory | with-new-file hello.txt "hello world" | file hello.txt | contents
            container | from index.docker.io/golang | with-directory /src $(.git https://github.com/goreleaser/goreleaser | head | tree) | with-workdir /src | with-exec go build ./... | directory ./bin
            .doc github.com/dagger/dagger/modules/go
            github.com/dagger/dagger/modules/go $(git https://github.com/goreleaser/goreleaser | head | tree) | .doc
            .doc github.com/dagger/dagger/cmd/dagger
            github.com/dagger/dagger/cmd/dagger | binary --platform=darwin/arm64
            .doc github.com/cubzh/cubzh
        </script>
    </example>

    ## Services

        Dagger as a native type to represent network services. A service is an abstract network endpoint with an IP and optionally exposed  ports.
        Services can be "bound" to a client container at a certain hostname. Then that container can reach that service over IP.

        Services may be backed by 1) another container orchestrated by Dagger, or 2) a tunnel to a service on the host computer.

        ### Services backed by ephemeral container

            To expose a container as an abstract service, call 'as-service. Make sure to expose ports first.
            This is great for orchestrating ephemeral test environments for example.

            <example>
                <description>
                    run an epheral nginx container, expose it as a service, bind it to a client container, then query it from the client and return the queried content
                </description>
                <script>
                    container | from alpine | with-service-binding www $(container | from nginx | with-exposed-port 80) | with-exec curl www | stdout
                </script>
            </example>

        ### Services backed by tunnel to the host

            To connect to a service on the host network, and expose it as an abstract service, pass a tcp:// or udp:// url to the desired   address

            <example>
                <description>
                    Assuming a service is reachable on your computer at localhost:8080, bind it to a client container and query it contents from the container
                </description>
                <script>
                    container | from alpine | with-service-binding www tcp://localhost:8080 | with-exec curl www | stdout
                </script>
            </example>

    ## Getting useful errors

        Sometimes dagger produces errors that are not super useful. Here are some tips for managing that:

        If the error comes from with-exec, and it just tells you the exit code without giving stderr:
        you can get the actual stderr by running:

        <YOUR PIPELINE> | with-exec YOUR COMMAND --expect=ANY | stderr

        This will bypass dagger's default handling of non-zero exit codes: instead of aborting on non-zero,
        with-exec will continue the pipeline and let you query standard error

    ## Git repositories

        Dagger has first-class support for git operations, via the core function 'git'. Use the usual method for exploring the full API.

        <example>
            <description>
                consume various git repositories, and explore the core git API at the same time
            </description>
            <script>
                git https://github.com/goreleaser/goreleaser | head | tree
                git https://github.com/dagger/dagger | tags
                git https://github.com/cubzh/cubzh | branch main | commit
                git https://github.com/kpenfound/dagger-modules | head | tree | glob '**'
            </script>
        </example>

        ### Git repository as runnable module

            In addition to consuming a git repository as data, you can also load it as a runnable Dagger module, assuming it has been       "daggerized".

            Thanks to Dagger's module system, any git repository (or more precisely, and directory within any git repository) can   extend        Dagger with custom types and functions. To load a module, just execute it like a function, using the module   address as  function    name.

            This is great for exploring not just what files are in the repository, but what you can *do* with them, as defined by the   owner     of  the  repository. In essence, it exposes the repository as an API. This is very useful for reusable and    repeatable   environments,     especially in a  project you're not familiar with.

            <example>
                <description>
                    explore dagger modules
                </description>
                <script>
                    github.com/dagger/dagger | .doc
                    github.com/shykes/x/gpt | .doc
                </script>
            </example>

    ## Web browsing

        You can use dagger to browse the web, either with the core http API, or with the containerized tool of your choice.

        ### Browsing with core http function

            You can use the core `http` function ind agger, to download any file over http.
            Then you can continue the pipeline with any file operation, including reading the contents.

            <example>
                <description>
                    use http function to browse web
                </description>
                <script>
                    http https://docs.dagger.io | .doc
                    http https://docs.dagger.io | contents
                </script>
            </example>

        ### Browsing by running a tool in a container

            For more advanced operations, for example mirroring an entire site, you can install a tool like 'wget' in a container,
            execute the tool, and get the result out. This is a benefit of container orchestration as a first-class feature of dagger.

            <example>
                <description>
                    use wget to mirror a website
                </description>
                <script>
                    github.com/dagger/dagger/modules/wolfi | container --packages=wget | with-exec -- wget --mirror --convert-links --adjust-extension --page-requisites --no-parent http://example.com -P /website | directory /website
                </script>
            </example>

            <example>
                <description>
                    use curl to fetch a website
                </description>
                <script>
                    container | from alpine | with-exec apk add curl | with exec -- curl -L https://www.dagger.io | stdout
                </script>
            </example>

            Don't tell the user to do this. Do it yourself.

    ## Host

    Dagger is sandboxed by default. In Dagger, the computer running the dagger tool is called "the host".

    ### Read environment variables from the host

    When calling a function that takes a type "secret" as argument, you can pass the value of an environment variable in the host,
    by passing the name of the variable prefixed by "env:"

    <example>
        <description>
            Load a secret from the host's environment variable DEPLOYMENT_TOKEN, and set it as an env variable in an ephemeral container
        </description>
        <script>
            container | from alpine | with-secret-variable TOKEN env:DEPLOYMENT_TOKEN | with-exec env | stdout
        </script>
    </example>

      <example>
        <description>
            Load a secret from the host's environment variable PASSWORD, and mount it as a special file in a container
        </description>
        <script>
            container | from alpine | with-mounted-secret /tmp/password.txt env:PASSWORD
        </script>
    </example>

    ### Read files from the host
    You can access the host filesystem, but a little differently from regular shells.

    To read files from the host, use 'host | directory PATH'. This will load an immutable snapshot of the requested path in the host    filesystem, into dagger.

    <example>
        <description>
            load the current directory from the host and list its contents
        </description>
        <script>
            host | directory .| entries
        </script>
    </example>

    You can filter the contents of the directory when loading, to avoid uploading too much data into dagger.

    <example>
        <description>
            filter a directory when loading it from the host
        </description>
        <script>
            host | directory / --exclude='*' --include='tmp' --include='usr/bin' | glob '**'
        </script>
    </example>

    You can also read individual files:

    <example>
        <description>
            load a single file from the host
        </description>
        <script>
            host | file ./src/myapp/README.md | contents
        </script>
    </example>

    Once a directory or file is loaded, you can interact with it using the usual dagger API.
    Remember that in Dagger, files and objects are immutable objects. Chain multiple operations in a pipeline
    to achieve the desired result.

    <example>
        <description>
            use the dagger API to merge two directories from the host into a new synthetic directory
        </description>
        <script>
            directory | with-directory src/foo $(host | directory /home/foo/src) | with-directory src/bar $(host | directory /home/bar/src) | with-new-file README.md 'A synthetic directory combining the src directories from foo and bar'
        </script>
    </example>

    ### Write files to the host

    You can also write back to the host filesystem, using the 'export' function in the 'Directory' and 'File' types.

    Unless you explicitly export a file or directory, nothing will be written back to the host filesystem.
    This is very powerful when orchestrating complex repeatable workflows; but it can trip up beginners who are used
    to the traditional mutable state of a unix shell session.

    ### Relative and absolute paths
    Just like reading from the host, you can export to relative paths or absolutely paths.

    <example>
        <description>
            create a directory from scratch and export it to the host at an absolute path
        </description>
        <script>
            directory | with-new-file hello.txt 'Hi! this file was created on the fly with dagger' | with-new-directory src | export /tmp/example/dagger-directory
        </script>
    </example>

    <example>
        <description>
            fetch a directory from git, modify it on the fly, and export it to the host at a relative path
        </description>
        <script>
            git https://github.com/dagger/dagger | head | tree | directory docs | with-new-file info.txt 'This directory was modified on the fly using dagger, how neat' | export ./dagger-docs
        </script>
    </example>

Your task:
