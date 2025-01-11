How to edit files

<prerequisite>Dagger manual</prerequisite>

To edit files, use dagger's file creation API.

<example>
    <description>Add a new file to a directory</description>
    <script>
        $MYDIR | with-new-file bla.txt "Hello world"
    </script>
</example>


<example>
    <description>Add a new file with multi-line contents</description>
    <script>
        $MYDIR | with-new-file bla.txt "This is the title

This is the first paragraph

And this is the second paragraph
"
    </script>
</example>

<example>
    <description>Create a single file in a new directory</description>
    <script>
        directory | with-new-file hello "hello world"
    </script>
</example>


<example>
    <description>Move a file</description>
    <script>
        $MYDIR | with-file new/path $($MYDIR | file old/path) | without-file old/path
    </script>
</example>
