# API

All API endpoints are configured to also return appropriate HTTP statuses. Many return additional information either inside a 'Message' field, or an 'Error' field, depending on the result of the call. 

## Login

`POST api/login` Accepts a JSON object that includes a username, a password, and optionally a JWT token.
```
{
    "Name": {string},
    "WithToken": {bool},
    "Password": {string},
    "Token": {string}
}
```
## Directory listing

`GET api/dir/{path}` returns a JSON-object representing a list of the underlying directories and files from the given root. Additional queries include ?dirOnly={false|true} and ?recDepth={0..99} that sets how many branches of depth are shown. The path is optional. The 'directory' object is the root-folder of the tree, and all the branches are included under the 'children' list of objects. Note that the 'children' object can be missing entirely if the parent directory has no children.
```
{
  "reply": {string},
  "directory": {
        "name": {string},
        "isDir": {bool},
        "children": [
            {"name":, "isDir":, "children":[]}, 
            {"name":, "isDir":, "children":[]}...
        ]
    }
}
```

## Streaming

`GET api/stream/{file}` Returns a data stream of the closest matching video file (if one is found). Optional query includes ?playlist={false|true} which instead returns a .m3x file that includes the paths to all the files in a given folder, sorted by episode number.