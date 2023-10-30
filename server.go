package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	

	"net/http"
	"strconv"
	"text/template"
	"time"

	"forum/helpers"

	_ "github.com/mattn/go-sqlite3"


)



type Post struct {
	ID           int
	Content      string
	Username     string
	Likes        int
	Dislikes     int
	CreatedAt    time.Time
	Comments     []Comment
	PostedAgo    string
	CommentCount int
}

type HomePageData struct {
	Username           string
	Posts              []Post
	Role               string
	ModerationRequests []string
	Moderators         []string
	ReportedRequests   int
}

type Comment struct {
	ID        int
	PostID    int
	Username  string
	Content   string
	Likes     int
	Dislikes  int
	CreatedAt time.Time
	PostedAgo string
}

type Vote struct {
	PostID   int    `json:"postID"`
	Username string `json:"username"`
}

func main() {

	db, err := helpers.GetDbConnection()
	if err != nil {
	  log.Fatalf("failed to prepare database connection: %v", err)
	}
	defer db.Close()

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.Handle("/dist/", http.StripPrefix("/dist/", http.FileServer(http.Dir("dist"))))

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) { handleCallback(w, r, db)})
	http.HandleFunc("/callbackgithub", func(w http.ResponseWriter, r *http.Request) { HandleCallbackGithub(w, r, db)})
	http.HandleFunc("/auth/google", helpers.HandleLogin)
	http.HandleFunc("/auth/github", handleGithubLogin)
	http.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) { reportPostHandler(w, r, db) })
	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) { deletePostHandler(w, r, db) })	
	http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) { admin(w, r, db) })
	http.HandleFunc("/submitpost", func(w http.ResponseWriter, r *http.Request) { createPost(w, r, db) })
	http.HandleFunc("/myposts", func(w http.ResponseWriter, r *http.Request) { showMyPostsHandler(w, r, db) })
	http.HandleFunc("/commentlike", commentLikeHandler)
	http.HandleFunc("/commentdislike", commentDislikeHandler)
	http.HandleFunc("/like", likeHandler)
	http.HandleFunc("/dislike", dislikeHandler)
	http.HandleFunc("/filterpage", filterPage)
	http.HandleFunc("/submitcomment", func(w http.ResponseWriter, r *http.Request) { submitComment(w, r, db) })
	http.HandleFunc("/addcomment", addComment)
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) { loginHandler(w, r, db) })
	http.HandleFunc("/homepage.html", func(w http.ResponseWriter, r *http.Request) { homePageHandler(w, r, db) })
	http.HandleFunc("/logout", logOutHandler)
	http.HandleFunc("/createpost", serveCreatePostPage)
	http.HandleFunc("/", homeHandler)

	fmt.Println("Server started on port 8080.")
	http.ListenAndServe(":8080", nil)
}



func handleGithubLogin(w http.ResponseWriter, r *http.Request) {
	 githubClientID := "25d7ff2314f58883dc2a"

	 // Create the dynamic redirect URL for login
	 redirectURL := fmt.Sprintf(
		 "https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s",
		 githubClientID,
		 "http://localhost:8080/callbackgithub",
		)
 
	 http.Redirect(w, r, redirectURL, 301)

}

func handleCallback(w http.ResponseWriter, r *http.Request, db *sql.DB) {

	if r.FormValue("state") != helpers.RandomState {
		fmt.Println("State is not valid")
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	token, err := helpers.GoogleOauthConfig.Exchange(context.Background(), r.FormValue("code"))

	if err != nil {
		fmt.Fprintf(w, "Could not get token: %s\n", err.Error())
		return
	}

	resp, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
	if err != nil {
		fmt.Println("Could not create get request: %s/n", err.Error())
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return	
	}

	defer resp.Body.Close()
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Could not  parse response : %s/n", err.Error())
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	} 

	var GoogleResponse helpers.GoogleResponse
	if err := json.Unmarshal(content, &GoogleResponse) ; err != nil {
		fmt.Printf("Error unmarshalling response: %s\n", err.Error())
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	helpers.SQLAuthorize(w,r,db,GoogleResponse.Name, GoogleResponse.Email)
	
}
func HandleCallbackGithub(w http.ResponseWriter, r *http.Request, db *sql.DB) {
    code := r.URL.Query().Get("code")

    githubAccessToken := helpers.GetGithubAccessToken(code)

    username := helpers.GetGithubData(githubAccessToken)

    helpers.SQLAuthorize(w,r,db,username,"")

}

func handleVote(w http.ResponseWriter, r *http.Request, voteType string, comment bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var vote Vote
	err := json.NewDecoder(r.Body).Decode(&vote)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	db, err := helpers.GetDbConnection()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get DB connection: %v", err), http.StatusInternalServerError)
	}
	defer db.Close()

	userSession, err := helpers.ValidateSessionFromCookie(w, r)
	if err != nil {
		http.Error(w, "Invalid username", http.StatusBadRequest)
		return
	}

	userID := helpers.SQLSelectUserID(db, userSession.Username)

	err = helpers.SQLinsertVote(vote.PostID, userID, voteType, comment)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	likesCount, err := helpers.SQLGetVotesCount(db, vote.PostID, "like", comment)
	if err != nil {
		http.Error(w, "failed to get likes count for post ID %d: %w", vote.PostID)
	}
	dislikesCount, err := helpers.SQLGetVotesCount(db, vote.PostID, "dislike", comment)
	if err != nil {
		http.Error(w, "failed to get likes count for post ID %d: %w", vote.PostID)
	}

	response := map[string]int{
		"likesCount":    likesCount,
		"dislikesCount": dislikesCount,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func likeHandler(w http.ResponseWriter, r *http.Request) {
	handleVote(w, r, "like", false)
}

func dislikeHandler(w http.ResponseWriter, r *http.Request) {
	handleVote(w, r, "dislike", false)
}

func commentLikeHandler(w http.ResponseWriter, r *http.Request) {
	handleVote(w, r, "like", true)
}

func commentDislikeHandler(w http.ResponseWriter, r *http.Request) {
	handleVote(w, r, "dislike", true)
}

func filterPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	categories, err := formValue(w, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed in formValue: %v", err), http.StatusBadRequest)
		return
	}

	db, err := helpers.GetDbConnection()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get DB connection: %v", err), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	posts, err := filterPosts(db, categories)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to filter posts: %v", err), http.StatusInternalServerError)
		return
	}

	comments, err := getCommentsFromDatabase(db)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to retrieve comments: %v", err), http.StatusInternalServerError)
		return
	}

	posts = addCommentsToPost(posts, comments)
	likesToPostsAndComments(db, posts)

	for _, v := range posts {
		likesCount, err := helpers.SQLGetVotesCount(db, v.ID, "like", false)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to get likes count for post ID %d: %v", v.ID, err), http.StatusInternalServerError)
			return
		}
		v.Likes = likesCount
	}

	data := HomePageData{
		Posts: posts,
	}
	// before i parsed the filteredPage here. Though i think it is actually unnecessary
	t, err := template.ParseFiles("templates/homepage.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = t.Execute(w, data)
	if err != nil {
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

func addCommentsToPost(posts []Post, comments []Comment) (modPosts []Post) {
	modPosts = make([]Post, len(posts))
	for i, post := range posts {
		modPosts[i] = post
		for _, comment := range comments {
			if posts[i].ID == comment.PostID {
				modPosts[i].Comments = append(modPosts[i].Comments, comment)
			}
		}
	}
	return modPosts
}

func admin(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	userSession, error := helpers.ValidateSessionFromCookie(w, r)

	if userSession == nil {
		helpers.DeleteCookie(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if userSession != nil {
		if userSession.Username != "Admin" {
			http.Redirect(w, r, "/", http.StatusForbidden)
			return
		}
		if error != nil {
			http.Error(w, "This is error", http.StatusBadRequest)
			return
		}
	}

	var moderationRequests []string

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing in Admin func", http.StatusBadRequest)
	}

	if r.Method == http.MethodPost {
		remove := r.FormValue("remove")
		accept := r.FormValue("accept")
		decline := r.FormValue("decline")

		if accept != "" {
			helpers.SQLAnswerModerationRequest(db, accept, "SetToModerator")
		} else if decline != "" {
			helpers.SQLAnswerModerationRequest(db, decline, "")
		} else {
			fmt.Println("Excuse-moi?")
		}

		if remove != "" {
			helpers.SQLAnswerModerationRequest(db, remove, "RemoveModeration")
		}
	}

	moderationRequests, err = helpers.SQLSelectModeratorRequest(db, false)
	moderators, err := helpers.SQLSelectModeratorRequest(db, true)

	data := HomePageData{
		Moderators:         moderators,
		ModerationRequests: moderationRequests,
	}

	t, err := template.ParseFiles("templates/admin.html")
	if err != nil {
		log.Printf("Error parsing template: %v", err)

		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = t.Execute(w, data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

func submitComment(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form", http.StatusBadGateway)
		return
	}
	comment := r.FormValue("comment")
	postID := r.FormValue("id")

	if postID == "" {
		http.Error(w, "missing post id", http.StatusBadRequest)
		return
	}

	userSession, err := helpers.ValidateSessionFromCookie(w, r)
	userID := helpers.SQLSelectUserID(db, userSession.Username)

	if comment != "" {
		if err := helpers.SQLInsertComment(db, postID, comment, userID); err != nil {
		}
	} else {
		http.Error(w, "Creating empty comment is forbidden.", http.StatusBadRequest)
	}

	http.Redirect(w, r, "homepage.html", http.StatusSeeOther)
}

func addComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form", http.StatusBadGateway)
		return
	}

	postID := r.FormValue("id")
	if postID == "" {
		http.Error(w, "missing post id", http.StatusBadRequest)
		return
	}
	t, err := template.ParseFiles("templates/addcomment.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = t.Execute(w, postID)
	if err != nil {
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

func createPost(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	categories, err := formValue(w, r)
	if err != nil {
		http.Error(w, "Failed in formValue", http.StatusBadRequest)
		return
	}

	postContent := r.FormValue("postContent")

	userSession, err := helpers.ValidateSessionFromCookie(w, r)

	userID := helpers.SQLSelectUserID(db, userSession.Username)
	if postContent != "" {
		if err := helpers.SQLInsertPost(db, postContent, userID); err != nil {
			http.Error(w, "failed to insert post: %w", http.StatusBadRequest)
		}
	} else {
		http.Error(w, "Creating empty post is forbidden.", http.StatusBadRequest)
	}
	postID, err := helpers.SQLLastPostID(db)
	if err != nil {
		http.Error(w, "Failed to get last post ID", http.StatusInternalServerError)
		return
	}
	helpers.SQLInsertCategorie(db, postID, categories)

	http.Redirect(w, r, "homepage.html", http.StatusSeeOther)
}

func serveCreatePostPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.ServeFile(w, r, "templates/createpost.html")
}

func logOutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	helpers.DeleteCookie(w, r)

	http.ServeFile(w, r, "templates/logout.html")
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.ServeFile(w, r, "templates/index.html")
}


func likesToPostsAndComments(db *sql.DB, posts []Post) error {
	for i := range posts {
		// Added time since post.
		postAgo := helpers.PostedAgo(posts[i].CreatedAt)
		posts[i].PostedAgo = postAgo
		posts[i].CommentCount = helpers.SQLGetCommentCount(db, posts[i].ID)

		likesCount, err := helpers.SQLGetVotesCount(db, posts[i].ID, "like", false)
		if err != nil {
			return fmt.Errorf("failed to get likes count for post ID %d: %w", posts[i].ID, err)
		}
		posts[i].Likes = likesCount

		dislikesCount, err := helpers.SQLGetVotesCount(db, posts[i].ID, "dislike", false)
		if err != nil {
			return fmt.Errorf("failed to get dislikes count for post ID %d: %w", posts[i].ID, err)
		}
		posts[i].Dislikes = dislikesCount

		for j := range posts[i].Comments {

			// adding time since comment.
			commentAgo := helpers.PostedAgo(posts[i].Comments[j].CreatedAt)
			posts[i].Comments[j].PostedAgo = commentAgo

			commentLikesCount, err := helpers.SQLGetVotesCount(db, posts[i].Comments[j].ID, "like", true)
			if err != nil {
				return fmt.Errorf("failed to get likes count for comment ID %d: %w", posts[i].Comments[j].ID, err)
			}
			posts[i].Comments[j].Likes = commentLikesCount

			commentDislikesCount, err := helpers.SQLGetVotesCount(db, posts[i].Comments[j].ID, "dislike", true)
			if err != nil {
				return fmt.Errorf("failed to get dislikes count for comment ID %d: %w", posts[i].Comments[j].ID, err)
			}
			posts[i].Comments[j].Dislikes = commentDislikesCount
		}
	}

	return nil
}

func homePageHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	posts, err := getPostsFromDatabase(db, "normal", "")
	comments, err := getCommentsFromDatabase(db)
	posts = addCommentsToPost(posts, comments)
	likesToPostsAndComments(db, posts)

	userSession, err := helpers.ValidateSessionFromCookie(w, r)
	var username string
	var role string
	var moderationRequests []string
	var count int
	if userSession != nil {
		username = userSession.Username
		role, err = helpers.SQLGetUserRole(db, userSession.Username)
		if err != nil {
			fmt.Println("Failed to get role")
		}
		moderationRequests, err = helpers.SQLSelectModeratorRequest(db, false)
		count, err = helpers.CountSQL(db, "reportedRequests", "")
	}

	// admin pw is Admin123
	data := HomePageData{
		Username:           username,
		Posts:              posts,
		Role:               role,
		ModerationRequests: moderationRequests,
		ReportedRequests:   count,
	}
	if err != nil {
		// if error exists, mean there is no session and show view page only.
		t, err := template.ParseFiles("templates/homepageview.html")
		if err != nil {
			fmt.Printf("Error parsing template %v:", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = t.Execute(w, data)
		if err != nil {
			fmt.Printf("Error executing template %v:", err)
			http.Error(w, "Error executing template", http.StatusInternalServerError)
			return
		}
	} else {

		t, err := template.ParseFiles("templates/homepage.html")
		if err != nil {
			fmt.Printf("Error parsing template %v:", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = t.Execute(w, data)
		if err != nil {
			fmt.Printf("Error executing template %v:", err)
			http.Error(w, "Error executing template", http.StatusInternalServerError)
			return
		}

	}
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var data any
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form", http.StatusBadGateway)
		return
	}

	username := r.FormValue("username")
	passwordRaw := r.FormValue("password")
	password, err := helpers.PasswordCrypter(passwordRaw)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
	}
	email := r.FormValue("email")
	appliesForModerator := r.FormValue("checkbox")

	var apply int

	if appliesForModerator != "" {
		apply = 1
	} else {
		apply = 0
	}

	err = helpers.InitalizeDb(username, string(password), email, "user", apply)
	if err != nil {
		errMessage, httpCode := helpers.ErrorCheck(err)
		data = struct {
			Message string
			Code    uint
		}{
			Message: errMessage,
			Code:    httpCode,
		}
	}

	t, err2 := template.ParseFiles("templates/register.html")
	if err2 != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err2 = t.Execute(w, data)
	if err2 != nil {
		http.Error(w, err2.Error(), http.StatusInternalServerError)
		return
	}
}



func loginHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	// if r.Method != http.MethodPost {
	// 	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	// 	return
	// }

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form", http.StatusBadGateway)
		return
	}

	username := r.FormValue("login-username")
	password := r.FormValue("login-password")

	row := db.QueryRow("SELECT password FROM users WHERE username = ?;", username)

	var hashedPassword string
	err = row.Scan(&hashedPassword) 

	if err == sql.ErrNoRows {
		http.Redirect(w, r, "/registration.html?error=Invalid username or password!", http.StatusSeeOther)
		return
	} else if err != nil {
		http.Error(w, "Failed to execute query", http.StatusInternalServerError)
		return
	}

	match, err := helpers.PasswordCheck(password, hashedPassword)

	if match == true {
		helpers.CreateSession(w, r, username) 
		http.Redirect(w, r, "homepage.html", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/registration.html?error=Invalid username or password!", http.StatusSeeOther)
	}
}



func getReportedPostsFromDatabase(db *sql.DB) ([]Post, error) {
	rows, err := db.Query("SELECT posts.id, posts.content, posts.created_at, users.username FROM posts JOIN users ON posts.user_id = users.id WHERE flagged = 1;")
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}

	if rows != nil {
		defer rows.Close()

		var posts []Post
		for rows.Next() {
			var post Post
			if err := rows.Scan(&post.ID, &post.Content, &post.CreatedAt, &post.Username); err != nil {
				return nil, fmt.Errorf("Failed to scan row: %v", err)
			}
			posts = append(posts, post)
		}

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("Failed after iterating rows: %v", err)
		}

		return posts, nil
	}

	return nil, fmt.Errorf("No rows to return")
}

func getPostsFromDatabase(db *sql.DB, postsQuery string, username string) ([]Post, error) {
	var rows *sql.Rows
	var err error

	if postsQuery == "normal" {
		rows, err = db.Query("SELECT posts.id, posts.content,  posts.created_at, users.username FROM posts JOIN users ON posts.user_id = users.id;")
		if err != nil {
			return nil, fmt.Errorf("Failed to execute query: %v", err)
		}
	} else if postsQuery == "myposts" {
		rows, err = db.Query(`SELECT posts.id, posts.content, posts.created_at, users.username 
		FROM posts 
		JOIN users ON posts.user_id = users.id 
		WHERE users.username = ?;`, username)
		if err != nil {
			return nil, fmt.Errorf("Failed to execute query: %v", err)
		}
	} else if postsQuery == "mylikedposts" {
		// Merci
		rows, err = db.Query(`SELECT 
		posts.id, 
		posts.content, 
		posts.created_at,
		authors.username 
		FROM 
		post_votes 
		JOIN 
		users as likers ON post_votes.user_id = likers.id 
		JOIN 
		posts ON post_votes.post_id = posts.id 
		JOIN 
		users as authors ON posts.user_id = authors.id 
		WHERE 
		likers.username = ? 
		AND post_votes.vote_type = 'like';
	`, username)
		if err != nil {
			return nil, fmt.Errorf("Failed to execute query: %v", err)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("Failed to execute query: %v", err)
	}

	if rows != nil {
		defer rows.Close()

		var posts []Post
		for rows.Next() {
			var post Post
			if err := rows.Scan(&post.ID, &post.Content, &post.CreatedAt, &post.Username); err != nil {
				return nil, fmt.Errorf("Failed to scan row: %v", err)
			}
			posts = append(posts, post)
		}

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("Failed after iterating rows: %v", err)
		}

		return posts, nil
	}

	return nil, fmt.Errorf("No rows to return")
}



func getCommentsFromDatabase(db *sql.DB) ([]Comment, error) {
	rows, err := db.Query("SELECT comments.id, comments.content, comments.created_at, posts.id AS post_id, users.username FROM comments JOIN posts ON comments.post_id = posts.id JOIN users ON comments.user_id = users.id;")
	if err != nil {
		return nil, fmt.Errorf("Failed to execute query: %v", err)
	}

	defer rows.Close()
	var comments []Comment
	for rows.Next() {
		var comment Comment
		if err := rows.Scan(&comment.ID, &comment.Content, &comment.CreatedAt, &comment.PostID, &comment.Username); err != nil {
			return nil, fmt.Errorf("Failed to scan row: %v", err)
		}
		comments = append(comments, comment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Failed after iterating rows: %v", err)
	}
	return comments, nil
}



func filterPosts(db *sql.DB, categories []int) (posts []Post, err error) {
	postMap := make(map[int]Post)
	for _, category := range categories {
		if category != 0 {
			rows, err := db.Query(`SELECT posts.id, posts.content, posts.created_at, users.username 
			FROM posts 
			JOIN post_categories 
			ON posts.id = post_categories.post_id 
			JOIN users ON posts.user_id = users.id
			WHERE post_categories.category_id = ?;`, category)
			if err != nil {
				return nil, fmt.Errorf("Failed to execute query: %v", err)
			}
			defer rows.Close()

			for rows.Next() {
				var post Post
				if err := rows.Scan(&post.ID, &post.Content, &post.CreatedAt, &post.Username); err != nil {
					return nil, fmt.Errorf("Failed to scan row: %v", err)
				}
				// If the post is not in the map, add it.
				if _, exists := postMap[post.ID]; !exists {
					postMap[post.ID] = post
				}
			}

			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("Failed after iterating rows: %v", err)
			}
		}
	}

	for _, post := range postMap {
		posts = append(posts, post)
	}

	return posts, nil
}

func formValue(w http.ResponseWriter, r *http.Request) ([]int, error) {
	err := r.ParseForm()
	if err != nil {
		return nil, fmt.Errorf("error parsing form: %v", err)
	}

	csValue := r.FormValue("counter-strike")
	lolValue := r.FormValue("league")
	rsValue := r.FormValue("runescape")

	var categories []int

	if csValue != "" {
		strCsValue, err := strconv.Atoi(csValue)
		if err != nil {
			return nil, fmt.Errorf("error converting counter-strike value: %v", err)
		}
		categories = append(categories, strCsValue)
	}

	if lolValue != "" {
		strLolValue, err := strconv.Atoi(lolValue)
		if err != nil {
			return nil, fmt.Errorf("error converting league value: %v", err)
		}
		categories = append(categories, strLolValue)
	}

	if rsValue != "" {
		strRsValue, err := strconv.Atoi(rsValue)
		if err != nil {
			return nil, fmt.Errorf("error converting runescape value: %v", err)
		}
		categories = append(categories, strRsValue)
	}

	return categories, nil
}

func showMyPostsHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error parsing form: %v", err), http.StatusBadRequest)
		return
	}

	formValue := r.FormValue("myposts")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}


	userSession, err := helpers.ValidateSessionFromCookie(w, r)
	var username string
	var role string
	if userSession != nil {
		username = userSession.Username
		role, err = helpers.SQLGetUserRole(db, userSession.Username)
	}

	posts, err := getPostsFromDatabase(db, formValue, userSession.Username)
	comments, err := getCommentsFromDatabase(db)

	posts = addCommentsToPost(posts, comments)

	likesToPostsAndComments(db, posts)

	data := HomePageData{
		Username: username,
		Posts:    posts,
		Role:     role,
	}

	t, err := template.ParseFiles("templates/homepage.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = t.Execute(w, data)
	if err != nil {
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

func deletePostHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var postID int
	var status string
	if r.FormValue("delete") != "" {
		postID, err = strconv.Atoi(r.FormValue("delete"))
		status = "delete"
	} else if r.FormValue("report") != "" {
		postID, err = strconv.Atoi(r.FormValue("report"))
		status = "report"
	} else {
		http.Error(w, "Either report or delete parameter required", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}


	err = helpers.SQLDeletePost(db, postID, status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirectLocation := "homepage.html"
	if status == "delete" {
		redirectLocation = "report"
	}
	http.Redirect(w, r, redirectLocation, http.StatusSeeOther)
}

func reportPostHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	userSession, err := helpers.ValidateSessionFromCookie(w, r)
	if err != nil {
		http.Error(w, "Failed to validate session from cookie: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var role string
	if userSession != nil {
		role, err = helpers.SQLGetUserRole(db, userSession.Username)
		if err != nil {
			http.Error(w, "Failed to get user role: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if role != "moderator" {
		helpers.DeleteCookie(w, r)
		http.Redirect(w, r, "/", http.StatusForbidden)
		return
	}

	posts, err := getReportedPostsFromDatabase(db)
	if err != nil {
		http.Error(w, "Failed to get reported posts from database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	comments, err := getCommentsFromDatabase(db)
	if err != nil {
		http.Error(w, "Failed to get comments from database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	posts = addCommentsToPost(posts, comments)
	err = likesToPostsAndComments(db, posts)
	if err != nil {
		http.Error(w, "Failed to add likes to posts and comments: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusInternalServerError)
		return
	}

	report := r.FormValue("report")
	delete := r.FormValue("delete")

	if report != "" {
		reportInt, err := strconv.Atoi(report)
		if err != nil {
			http.Error(w, "Failed to convert report to integer: "+err.Error(), http.StatusInternalServerError)
			return
		}
		err = helpers.SQLDeletePost(db, reportInt, "report")
		if err != nil {
			http.Error(w, "Failed to report post: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else if delete != "" {
		deleteInt, err := strconv.Atoi(delete)
		if err != nil {
			http.Error(w, "Failed to convert delete to integer: "+err.Error(), http.StatusInternalServerError)
			return
		}
		err = helpers.SQLDeletePost(db, deleteInt, "delete")
		if err != nil {
			http.Error(w, "Failed to delete post: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	data := HomePageData{
		Posts: posts,
		Role:  role,
	}

	t, err := template.ParseFiles("templates/reportedposts.html")
	if err != nil {
		http.Error(w, "Failed to parse template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = t.Execute(w, data)
	if err != nil {
		http.Error(w, "Failed to execute template: "+err.Error(), http.StatusInternalServerError)
	}
}
