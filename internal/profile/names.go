package profile

// Списки для генераторов. Расширяйте как нужно: чем длиннее списки,
// тем меньше повторов между аккаунтами.

var maleNames = []string{
	"James", "Michael", "Daniel", "Matthew", "Andrew", "Joshua", "Ryan", "Nathan",
	"Ethan", "Tyler", "Brandon", "Justin", "Kevin", "Jason", "Eric", "Adam",
	"Brian", "Aaron", "Jacob", "Logan", "Dylan", "Caleb", "Owen", "Luke",
}

var femaleNames = []string{
	"Emily", "Sarah", "Jessica", "Ashley", "Megan", "Hannah", "Lauren", "Rachel",
	"Olivia", "Emma", "Grace", "Natalie", "Samantha", "Amanda", "Nicole", "Kayla",
	"Chloe", "Abigail", "Madison", "Brooke", "Taylor", "Alexis", "Julia", "Sophie",
}

var lastNames = []string{
	"Miller", "Anderson", "Taylor", "Thomas", "Moore", "Jackson", "Martin", "Thompson",
	"White", "Harris", "Clark", "Lewis", "Robinson", "Walker", "Young", "Allen",
	"King", "Wright", "Scott", "Green", "Baker", "Adams", "Nelson", "Carter",
	"Mitchell", "Turner", "Parker", "Collins", "Stewart", "Morris", "Rogers", "Reed",
}

// Не длиннее 70 символов (лимит bio без Premium).
var bios = []string{
	"Coffee, music and long walks.",
	"Just here for good conversations.",
	"Keeping it simple.",
	"Dog person. Tea over coffee.",
	"Learning something new every day.",
	"Weekend hiker.",
	"Music on, worries off.",
	"Bookworm. Always up for a chat.",
	"Making the most of every day.",
	"Foodie and traveler.",
	"Gym, books, repeat.",
	"Photography enthusiast.",
	"Movie nights and road trips.",
	"Easygoing. Open to new ideas.",
	"Sunsets and good vibes.",
}
